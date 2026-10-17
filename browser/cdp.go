// Package browser connects read-only to existing Chrome debugging targets.
// CDP transport messages use JSON by protocol definition; application settings
// are exclusively YAML.
package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/remote"
)

// Tab identifies an existing Chrome page. CDP TargetInfo does NOT provide a
// historical last-accessed timestamp; future observed activity remains separate.
type Tab struct {
	TargetID string
	Title    string
	URL      string
	Domain   string
}

// Client never launches Chrome or creates tabs. The constructor restricts the
// DevTools HTTP endpoint to loopback; an exposed CDP port is effectively remote
// control of the browser and must never be treated as a public API.
type Client struct {
	endpoint *url.URL
	http     *http.Client
	timeout  time.Duration
}

func NewClient(rawEndpoint string, timeout time.Duration) (*Client, error) {
	endpoint, err := url.Parse(strings.TrimSpace(rawEndpoint))
	if err != nil {
		return nil, fmt.Errorf("parse CDP endpoint: %w", err)
	}
	if endpoint.Scheme != "http" || endpoint.Host == "" || endpoint.User != nil ||
		(endpoint.Path != "" && endpoint.Path != "/") || endpoint.RawQuery != "" || endpoint.Fragment != "" ||
		!loopbackHost(endpoint.Hostname()) {
		return nil, errors.New("CDP endpoint must be an http:// loopback host and port with no path, credentials, or query")
	}
	if endpoint.Port() == "" {
		return nil, errors.New("CDP endpoint must specify an explicit port")
	}
	if timeout <= 0 {
		return nil, errors.New("CDP timeout must be positive")
	}
	return &Client{endpoint: endpoint, http: &http.Client{
		Timeout: timeout,
		// Never follow a redirect from the loopback debugger to another
		// host, which could leak CDP discovery data off-machine.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, timeout: timeout}, nil
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type versionResponse struct {
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

type pageResponse struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// jsonGET only reads the local debugging service's discovery endpoints.
// The bounded read prevents a malformed service from consuming unlimited RAM.
func (c *Client) jsonGET(ctx context.Context, path string, result any) error {
	address := c.endpoint.ResolveReference(&url.URL{Path: path}).String()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: Chrome debugging endpoint returned HTTP %d", path, resp.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if err := decoder.Decode(result); err != nil {
		return fmt.Errorf("decode Chrome %s: %w", path, err)
	}
	return nil
}

// Discover starts from Chrome's read-only HTTP discovery API because creating
// an unbound chromedp page context would otherwise open a *new* Chrome tab.
// It attaches to one ALREADY EXISTING page target, then issues the requested
// Target.getTargets CDP method and discards the bootstrap snapshot.
// Discover is the original lightweight, read-only listing API.
func (c *Client) Discover(ctx context.Context) ([]Tab, error) {
	var tabs []Tab
	err := c.withTargets(ctx, func(_ context.Context, found []Tab) error {
		tabs = found
		return nil
	})
	return tabs, err
}

// withTargets owns one remote browser session and shares it with any sampling
// children. Every context attaches to a user-owned tab in detach-only mode.
// This prevents unnecessary WebSocket connections and accidental tab closure.
func (c *Client) withTargets(ctx context.Context, visit func(context.Context, []Tab) error) error {
	return c.withTargetsAvoid(ctx, "", visit)
}

// withTargetsAvoid keeps lifecycle target handling off the bootstrap page.
// Opening or resuming a frozen target through the chromedp bootstrap can stall
// while it initializes Runtime. Require a different existing page instead;
// Chrome Optimizer never creates a recovery tab.
func (c *Client) withTargetsAvoid(ctx context.Context, avoidID string, visit func(context.Context, []Tab) error) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var version versionResponse
	if err := c.jsonGET(ctx, "/json/version", &version); err != nil {
		return fmt.Errorf("Chrome CDP endpoint %s is unavailable; on macOS run `make run-debug` to launch an isolated debugging profile (not your normal Chrome tabs): %w", c.endpoint.String(), err)
	}
	ws, err := url.Parse(version.WebSocketDebuggerURL)
	if err != nil || ws.Scheme != "ws" || !loopbackHost(ws.Hostname()) ||
		ws.User != nil || ws.Port() == "" || ws.Port() != c.endpoint.Port() ||
		!strings.HasPrefix(ws.Path, "/devtools/browser/") || ws.RawQuery != "" || ws.Fragment != "" {
		return errors.New("Chrome returned an invalid or non-loopback browser WebSocket debugger URL")
	}
	var discovered []pageResponse
	if err := c.jsonGET(ctx, "/json/list", &discovered); err != nil {
		return err
	}
	bootstrapID := pickBootstrapTarget(discovered, avoidID)
	if bootstrapID == "" {
		if avoidID != "" {
			return errors.New("lifecycle action needs another open page as a CDP helper; open an additional tab in the debugging Chrome window and retry")
		}
		// No page exists to attach without creating a new tab.
		return visit(ctx, []Tab{})
	}

	// Version 0.21 uses chromedp/remote and the typed cdproto Call API.
	// NoModifyURL prevents a second HTTP URL-resolution request; the browser
	// debugger URL was validated above as local and on the expected port.
	allocatorCtx, cancelAllocator := remote.NewAllocator(ctx, ws.String(), remote.NoModifyURL)
	defer cancelAllocator()
	tabCtx, cancelTab := chromedp.NewContext(allocatorCtx,
		chromedp.WithTargetID(target.ID(bootstrapID)),
		// chromedp v0.21 closes attached existing tabs by default on
		// cancellation. This tool does not own Chrome's tabs: detach only.
		chromedp.WithDetachOnCancel(),
	)
	defer cancelTab()
	var infos []*target.Info
	if err := chromedp.Do(tabCtx, chromedp.Func(func(runCtx context.Context, _ *chromedp.Target) error {
		// Target is a browser-scope domain. Calling it through the browser
		// session avoids incorrectly treating this as page instrumentation.
		result, err := cdp.Call(runCtx, chromedp.FromContext(runCtx).Browser,
			target.GetTargets, target.GetTargetsParams{})
		if err != nil {
			return err
		}
		infos = result.TargetInfos
		return nil
	})); err != nil {
		return fmt.Errorf("Target.getTargets (bootstrap target %s): %w", bootstrapID, err)
	}

	tabs := make([]Tab, 0, len(infos))
	for _, info := range infos {
		if info == nil || info.Type != "page" || info.TargetID == "" {
			continue
		}
		tabs = append(tabs, Tab{
			TargetID: string(info.TargetID),
			URL:      info.URL,
			Title:    info.Title,
			Domain:   Domain(info.URL),
		})
	}
	sort.Slice(tabs, func(i, j int) bool { return tabs[i].TargetID < tabs[j].TargetID })
	return visit(tabCtx, tabs)
}

// pickBootstrapTarget only attaches to a pre-existing page. Lifecycle
// operations must never bootstrap through their selected target, which could
// already be frozen and unable to run chromedp's initialization commands.
func pickBootstrapTarget(discovered []pageResponse, avoidID string) string {
	for _, candidate := range discovered {
		if candidate.Type == "page" && candidate.ID != "" && candidate.ID != avoidID {
			return candidate.ID
		}
	}
	return ""
}

// Domain provides the parsed hostname, not a guessed registrable domain.
// Non-network URLs retain a recognizable scheme instead of being misreported.
func Domain(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "-"
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "(invalid)"
	}
	// Internal Chrome/file/about URLs have no web domain, even if they
	// happen to contain a host component in their scheme syntax.
	if u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "ws" || u.Scheme == "wss" {
		if host := u.Hostname(); host != "" {
			return strings.ToLower(host)
		}
	}
	if u.Scheme != "" {
		return strings.ToLower(u.Scheme) + ":"
	}
	return "(relative)"
}
