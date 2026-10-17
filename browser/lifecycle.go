package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// LifecycleAction changes only the selected page's web lifecycle. Unlike
// operating system signals, these commands cannot suspend Chrome's process.
// Page.setWebLifecycleState is experimental and may be unsupported by Chrome.
type LifecycleAction string

const (
	Freeze LifecycleAction = "freeze"
	Resume LifecycleAction = "resume"
)

func (a LifecycleAction) valid() bool {
	return a == Freeze || a == Resume
}

// SetLifecycle explicitly requests a state transition for an exact TargetID.
// Read-only discovery and metric sampling never call this method.
//
// An independent page must already exist to bootstrap the CDP browser session:
// the selected tab could itself be frozen and therefore unable to initialize
// chromedp's Runtime. The independent page is attached to but never mutated.
// All contexts use WithDetachOnCancel to preserve existing tabs.
func (c *Client) SetLifecycle(ctx context.Context, targetID string, action LifecycleAction) (Tab, error) {
	if targetID == "" {
		return Tab{}, errors.New("a complete target ID is required; use make run to list tabs")
	}
	if !action.valid() {
		return Tab{}, fmt.Errorf("unsupported lifecycle action %q", action)
	}
	var selected Tab
	err := c.withTargetsAvoid(ctx, targetID, func(parent context.Context, tabs []Tab) error {
		tab, err := selectLifecycleTarget(tabs, targetID)
		if err != nil {
			return err
		}
		selected = tab
		tabCtx, cancel := chromedp.NewContext(parent,
			chromedp.WithTargetID(target.ID(targetID)),
			chromedp.WithDetachOnCancel(),
		)
		defer cancel()
		return chromedp.Do(tabCtx, chromedp.Func(func(commandCtx context.Context, session *chromedp.Target) error {
			inspect := func(ctx context.Context) (string, error) {
				// This reads only the current page's visibility and URL
				// scheme. It cannot create a new tab or change focus.
				boolean := true
				response, err := cdp.Call(ctx, session, runtime.Evaluate, runtime.EvaluateParams{
					Expression:    "document.visibilityState + ':' + location.protocol",
					ReturnByValue: &boolean,
					Silent:        &boolean,
				})
				if err != nil {
					return "", err
				}
				if response.ExceptionDetails != nil || response.Result == nil {
					return "", errors.New("page visibility unavailable")
				}
				var value string
				if err := json.Unmarshal(response.Result.Value, &value); err != nil {
					return "", fmt.Errorf("invalid page visibility response: %w", err)
				}
				return value, nil
			}
			change := func(ctx context.Context, operation LifecycleAction) error {
				state := page.SetWebLifecycleStateStateFrozen
				if operation == Resume {
					state = page.SetWebLifecycleStateStateActive
				}
				_, err := cdp.Call(ctx, session, page.SetWebLifecycleState,
					page.SetWebLifecycleStateParams{State: state})
				return err
			}
			return applyLifecycle(commandCtx, tab, action, inspect, change)
		}))
	})
	if err != nil {
		return Tab{}, fmt.Errorf("%s target %s: %w", action, targetID, err)
	}
	return selected, nil
}

// selectLifecycleTarget requires an exact, unique page ID. URL, domain and
// title are not selectors: multiple pages can share the same domain/title.
func selectLifecycleTarget(tabs []Tab, id string) (Tab, error) {
	var found Tab
	count := 0
	for _, tab := range tabs {
		if tab.TargetID == id {
			count++
			found = tab
		}
	}
	if count != 1 {
		return Tab{}, fmt.Errorf("target ID %q not found uniquely among current page tabs (found %d)", id, count)
	}
	return found, nil
}

// applyLifecycle contains all permissions and sequencing so it can be tested
// with a fake CDP adapter and is also the exact gate used by production code.
// Resuming is intentionally allowed without visibility checks: a frozen page
// may not be able to run JavaScript at all.
//
// IMPORTANT: visibility can change after inspection. This is a best-effort
// foreground guard, not an atomic guarantee against user tab switching.
func applyLifecycle(
	ctx context.Context, tab Tab, action LifecycleAction,
	inspect func(context.Context) (string, error),
	change func(context.Context, LifecycleAction) error,
) error {
	if !action.valid() {
		return fmt.Errorf("unsupported lifecycle action %q", action)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if action == Freeze {
		parsed, err := url.Parse(tab.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
			return errors.New("freeze supports ordinary http(s) pages only; Chrome internal, extension, file and blank pages are protected")
		}
		visibility, err := inspect(ctx)
		if err != nil {
			return fmt.Errorf("cannot verify whether page is in the background; refusing freeze: %w", err)
		}
		if visibility != "hidden:http:" && visibility != "hidden:https:" {
			return fmt.Errorf("page is not confirmed hidden (visibility=%q); refusing freeze to protect foreground pages", visibility)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := change(ctx, action); err != nil {
		return fmt.Errorf("Page.setWebLifecycleState is unavailable or rejected the request: %w", err)
	}
	return nil
}
