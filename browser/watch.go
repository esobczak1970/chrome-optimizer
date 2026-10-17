package browser

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/performance"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// maxWatchSessions limits retained CDP target subscriptions. All browser
// target metadata is still discovered; an excess tab shows a clear per-target
// status rather than silently increasing Chrome instrumentation overhead.
const maxWatchSessions = 64

type watchedTarget struct {
	ctx     context.Context
	cancel  context.CancelFunc
	epoch   uint64
	enabled bool
}

func newWatchedTarget(parent context.Context, targetID string, epoch uint64) *watchedTarget {
	tabCtx, cancel := chromedp.NewContext(parent,
		chromedp.WithTargetID(target.ID(targetID)),
		// Watch must never close existing Chrome tabs on exit or failure.
		chromedp.WithDetachOnCancel(),
	)
	return &watchedTarget{ctx: tabCtx, cancel: cancel, epoch: epoch}
}

// sample enables Performance only once per target attachment, then reuses
// that exact session for subsequent watch samples. That makes TaskDuration
// deltas comparable within an epoch, unlike one-shot Collect's fresh session.
func (s *watchedTarget) sample(tab Tab, timeout time.Duration) TabInfo {
	result := TabInfo{Tab: tab, SampledAt: time.Now(), MetricSessionEpoch: s.epoch}
	// chromedp initializes its target reader on the Do context. A timed
	// child of s.ctx would end the target reader on the first sample and
	// recreate the exact reset bug this watch mode is intended to fix.
	// Instead use the persistent context and cancel that session only if
	// this sample exceeds its timeout; next refresh can reattach cleanly.
	watchdog := time.AfterFunc(timeout, s.cancel)
	defer watchdog.Stop()
	err := chromedp.Do(s.ctx, chromedp.Func(func(runCtx context.Context, current *chromedp.Target) error {
		commandCtx, done := context.WithTimeout(runCtx, timeout)
		defer done()
		if !s.enabled {
			if _, err := cdp.Call(commandCtx, current, performance.Enable, performance.EnableParams{}); err != nil {
				return fmt.Errorf("Performance.enable: %w", err)
			}
			s.enabled = true
		}
		values, err := cdp.Call(commandCtx, current, performance.GetMetrics, cdp.Empty{})
		if err != nil {
			return fmt.Errorf("Performance.getMetrics: %w", err)
		}
		result.JSHeapUsedBytes, result.TaskDurationSeconds = parseMetrics(values.Metrics)
		result.SampledAt = time.Now()
		return nil
	}))
	if err != nil {
		result.MetricsError = err.Error()
	}
	return result
}

func (s *watchedTarget) close() {
	// Detaching is sufficient cleanup for the Performance subscription.
	// Do not issue Performance.disable while another CDP client might be
	// observing the same renderer, or attempt any page-close operation.
	s.cancel()
}

// watchSampler owns CDP target attachments for exactly one browser connection.
// This state and its callback are only used by the current monitoring command,
// never shared globally or persisted to disk.
type watchSampler struct {
	parent   context.Context
	timeout  time.Duration
	sessions map[string]*watchedTarget
	next     uint64
}

func newWatchSampler(parent context.Context, timeout time.Duration) *watchSampler {
	return &watchSampler{
		parent:   parent,
		timeout:  timeout,
		sessions: make(map[string]*watchedTarget),
	}
}

func (w *watchSampler) close() {
	for id, session := range w.sessions {
		session.close()
		delete(w.sessions, id)
	}
}

// sample first reconciles the current live TargetIDs with retained
// instrumentation sessions. A new/closed/failed target gets a new epoch.
// A failed sample never prevents healthy tabs from being rendered.
func (w *watchSampler) sample(tabs []Tab) []TabInfo {
	live := make(map[string]bool, len(tabs))
	for _, tab := range tabs {
		live[tab.TargetID] = true
	}
	for id, session := range w.sessions {
		if !live[id] {
			session.close()
			delete(w.sessions, id)
		}
	}
	for _, tab := range tabs {
		if _, exists := w.sessions[tab.TargetID]; exists || len(w.sessions) >= maxWatchSessions {
			continue
		}
		w.next++
		w.sessions[tab.TargetID] = newWatchedTarget(w.parent, tab.TargetID, w.next)
	}
	infos := collectConcurrent(w.parent, tabs, func(_ context.Context, tab Tab) TabInfo {
		session := w.sessions[tab.TargetID]
		if session == nil {
			return TabInfo{Tab: tab, MetricsError: "watch instrumentation limit reached (64 target sessions)"}
		}
		return session.sample(tab, w.timeout)
	})
	// Never retain failed target sessions, including sessions where a page
	// navigated or its renderer changed. Reattachment creates a new epoch.
	for _, info := range infos {
		if info.MetricsError != "" {
			if session, ok := w.sessions[info.TargetID]; ok {
				session.close()
				delete(w.sessions, info.TargetID)
			}
		}
	}
	return infos
}

// Watch keeps Performance.enabled on each existing tab between refreshes.
// This avoids repeated CDP handshakes and meaningless cross-session CPU/task
// comparisons. The callback receives complete unfiltered snapshots so a UI
// may filter without corrupting in-memory activity history.
//
// Watch is read-only: it never creates tabs, navigates, freezes or signals
// any Chrome process. It returns on context cancellation or callback error.
func (c *Client) Watch(ctx context.Context, interval time.Duration, onSample func([]TabInfo) error) error {
	if interval < time.Second || interval > time.Minute {
		return fmt.Errorf("watch interval %s must be between 1s and 1m", interval)
	}
	if onSample == nil {
		return errors.New("watch requires a sample callback")
	}
	err := c.withTargetSession(ctx, "", func(parent context.Context, initial []Tab) error {
		if len(initial) == 0 {
			return errors.New("Chrome has no page to attach; open a page in the debugging profile and retry")
		}
		sampler := newWatchSampler(parent, c.timeout)
		defer sampler.close()
		if err := onSample(sampler.sample(initial)); err != nil {
			return err
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				tabs, err := c.currentTargets(parent)
				if err != nil {
					if ctx.Err() != nil {
						return nil
					}
					return err
				}
				if err := onSample(sampler.sample(tabs)); err != nil {
					return err
				}
			}
		}
	})
	if ctx.Err() != nil {
		return nil
	}
	return err
}
