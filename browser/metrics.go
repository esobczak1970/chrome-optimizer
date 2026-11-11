// Package browser implements read-only Chrome DevTools Protocol diagnostics.
package browser

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/performance"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// TabInfo deliberately distinguishes JavaScript heap bytes from a renderer's
// physical RSS and TaskDuration's cumulative seconds from instantaneous CPU%.
// CDP has no historical "last used" timestamp. ObservedTaskActivity is a
// monitoring-derived task counter change, NOT proof of user interaction.
type TabInfo struct {
	Tab
	JSHeapUsedBytes     *uint64
	TaskDurationSeconds *float64
	// MetricSource is populated only after a successful CDP metric response.
	// A missing metric is distinguished from an invalid one via field errors.
	MetricSource      string
	JSHeapError       string
	TaskDurationError string
	// TaskDeltaSeconds is the measured increase in cumulative CDP task time.
	// TaskSampleWindowSeconds is the elapsed interval between valid readings.
	// Neither field represents CPU percentage or historical user activity.
	TaskDeltaSeconds        *float64
	TaskSampleWindowSeconds *float64
	ObservedTaskActivityAt  *time.Time
	// This source is known only after observing a valid task-counter increase.
	// It must never be described as historical tab access or user interaction.
	ObservedTaskActivitySource string
	// MetricSessionEpoch is nonzero for a persistent watch attachment.
	// A changed epoch invalidates TaskDuration comparisons after reattach.
	MetricSessionEpoch uint64
	SampledAt          time.Time
	MetricsError       string
}

const (
	MetricSourcePerformance                = "CDP Performance.getMetrics"
	ObservedTaskActivitySourceTaskDuration = "observed TaskDuration increase (not user interaction)"
)

// Collect attaches to an existing browser and samples its existing page
// targets. The shared parent holds one WebSocket connection; child contexts
// must use WithDetachOnCancel to avoid closing user-owned tabs on shutdown.
func (c *Client) Collect(ctx context.Context) ([]TabInfo, error) {
	var result []TabInfo
	err := c.withTargets(ctx, func(parent context.Context, tabs []Tab) error {
		result = collectConcurrent(parent, tabs, func(sampleCtx context.Context, tab Tab) TabInfo {
			return sampleTarget(sampleCtx, parent, tab)
		})
		return nil
	})
	return result, err
}

const maxConcurrentSamples = 4

// collectConcurrent keeps target identity/order stable even if a tab vanishes
// between Target.getTargets and Performance.getMetrics. Errors stay per-tab,
// so one inaccessible tab does not hide healthy metrics for other tabs.
// A private sampler is injected only at this boundary for deterministic tests.
func collectConcurrent(ctx context.Context, tabs []Tab, sample func(context.Context, Tab) TabInfo) []TabInfo {
	out := make([]TabInfo, len(tabs))
	for i, tab := range tabs {
		out[i] = TabInfo{Tab: tab, MetricsError: "collection canceled"}
	}
	if len(tabs) == 0 {
		return out
	}

	workers := len(tabs)
	if workers > maxConcurrentSamples {
		workers = maxConcurrentSamples
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					continue
				}
				info := sample(ctx, tabs[i])
				// The caller's target identity is authoritative, even if
				// an asynchronously closing tab changes its metadata.
				info.Tab = tabs[i]
				out[i] = info
			}
		}()
	}
send:
	for i := range tabs {
		select {
		case <-ctx.Done():
			break send
		case jobs <- i:
		}
	}
	close(jobs)
	wg.Wait()
	return out
}

func sampleTarget(ctx, parent context.Context, tab Tab) TabInfo {
	result := TabInfo{Tab: tab, SampledAt: time.Now()}
	// The parent has an initialized Browser transport. A new child attaches
	// to the existing page, never creates a new tab, and DETACHES ON CANCEL.
	tabCtx, cancel := chromedp.NewContext(parent,
		chromedp.WithTargetID(target.ID(tab.TargetID)),
		chromedp.WithDetachOnCancel(),
	)
	defer cancel()

	err := chromedp.Do(tabCtx, chromedp.Func(func(commandCtx context.Context, session *chromedp.Target) error {
		return readOneShotMetrics(commandCtx, &result, newPerformanceCommands(session))
	}))
	if err != nil {
		result.MetricsError = err.Error()
	}
	return result
}

// performanceCommands binds enable/read/disable to one exact target session.
// A tiny command adapter makes ordering and failure cleanup testable without
// opening real Chrome or weakening the detach-only target lifecycle.
type performanceCommands struct {
	enable  func(context.Context) error
	read    func(context.Context) ([]*performance.Metric, error)
	disable func(context.Context) error
}

func newPerformanceCommands(session *chromedp.Target) performanceCommands {
	return performanceCommands{
		enable: func(ctx context.Context) error {
			_, err := cdp.Call(ctx, session, performance.Enable, performance.EnableParams{})
			return err
		},
		read: func(ctx context.Context) ([]*performance.Metric, error) {
			response, err := cdp.Call(ctx, session, performance.GetMetrics, cdp.Empty{})
			if err != nil {
				return nil, err
			}
			return response.Metrics, nil
		},
		disable: func(ctx context.Context) error {
			_, err := cdp.Call(ctx, session, performance.Disable, cdp.Empty{})
			return err
		},
	}
}

// readOneShotMetrics always attempts to disable successful instrumentation,
// including after a failed get. Cancellation still detaches the target context
// if the protocol can no longer accept a disable command.
func readOneShotMetrics(ctx context.Context, info *TabInfo, commands performanceCommands) error {
	if err := commands.enable(ctx); err != nil {
		return fmt.Errorf("Performance.enable: %w", err)
	}
	defer func() { _ = commands.disable(ctx) }()
	metrics, err := commands.read(ctx)
	if err != nil {
		return fmt.Errorf("Performance.getMetrics: %w", err)
	}
	applyPerformanceMetrics(info, metrics)
	info.SampledAt = time.Now()
	return nil
}

// metricReadings keeps missing and malformed CDP measurements distinct without
// assuming that every page target supports both Performance metrics.
type metricReadings struct {
	heap      *uint64
	task      *float64
	heapError string
	taskError string
}

// parseMetricsDetailed accepts only finite, nonnegative values. Heap readings
// are byte counts and must be integral and representable as uint64. Invalid
// duplicates cannot overwrite a valid metric encountered earlier in the same
// response; missing values never become zero.
func parseMetricsDetailed(metrics []*performance.Metric) metricReadings {
	out := metricReadings{
		heapError: "JSHeapUsedSize missing",
		taskError: "TaskDuration missing",
	}
	for _, m := range metrics {
		if m == nil {
			continue
		}
		switch m.Name {
		case "JSHeapUsedSize":
			if math.IsNaN(m.Value) || math.IsInf(m.Value, 0) || m.Value < 0 ||
				m.Value >= math.Exp2(64) || math.Trunc(m.Value) != m.Value {
				if out.heap == nil {
					out.heapError = "JSHeapUsedSize invalid"
				}
				continue
			}
			value := uint64(m.Value)
			out.heap = &value
			out.heapError = ""
		case "TaskDuration":
			if math.IsNaN(m.Value) || math.IsInf(m.Value, 0) || m.Value < 0 {
				if out.task == nil {
					out.taskError = "TaskDuration invalid"
				}
				continue
			}
			value := m.Value
			out.task = &value
			out.taskError = ""
		}
	}
	return out
}

// parseMetrics retains the original two-value helper for existing clients and
// tests; live sampling uses the detailed diagnostics below.
func parseMetrics(metrics []*performance.Metric) (*uint64, *float64) {
	readings := parseMetricsDetailed(metrics)
	return readings.heap, readings.task
}

func applyPerformanceMetrics(info *TabInfo, metrics []*performance.Metric) {
	readings := parseMetricsDetailed(metrics)
	info.JSHeapUsedBytes = readings.heap
	info.TaskDurationSeconds = readings.task
	info.JSHeapError = readings.heapError
	info.TaskDurationError = readings.taskError
	info.MetricSource = MetricSourcePerformance
}

// TaskActivityTracker remembers the last observed positive TaskDuration delta.
// No browser history, URL, or title is persisted. It is suitable for a watch
// session; the initial sample alone cannot establish a "last used" timestamp.
type taskBaseline struct {
	duration  float64
	epoch     uint64
	sampledAt time.Time
}

type TaskActivityTracker struct {
	previous map[string]taskBaseline
	last     map[string]time.Time
}

func NewTaskActivityTracker() *TaskActivityTracker {
	return &TaskActivityTracker{
		previous: make(map[string]taskBaseline),
		last:     make(map[string]time.Time),
	}
}

// Observe compares samples at a known wall clock time. A reset or a vanished
// target clears history rather than misrepresenting an old measurement.
// Only task execution activity is inferred: background tasks may also qualify.
// Observe compares only valid, chronologically ordered counter readings from
// one stable Performance session. Invalid/error samples discard the baseline;
// missing evidence never becomes a fabricated last-used timestamp.
func (a *TaskActivityTracker) Observe(infos []TabInfo, now time.Time) {
	seen := make(map[string]bool, len(infos))
	for i := range infos {
		info := &infos[i]
		id := info.TargetID
		seen[id] = true
		info.ObservedTaskActivityAt = nil
		info.ObservedTaskActivitySource = ""
		info.TaskDeltaSeconds = nil
		info.TaskSampleWindowSeconds = nil

		current := info.TaskDurationSeconds
		if info.MetricsError != "" || current == nil || math.IsNaN(*current) || math.IsInf(*current, 0) || *current < 0 {
			delete(a.previous, id)
			delete(a.last, id)
		} else {
			observedAt := info.SampledAt
			if observedAt.IsZero() {
				// Synthetic callers may supply only the timestamp for this
				// observation; real CDP samples always contain SampledAt.
				observedAt = now
			}
			if prior, ok := a.previous[id]; ok {
				switch {
				case prior.epoch != info.MetricSessionEpoch:
					// Never compare unrelated Performance.enable sessions.
					delete(a.last, id)
				case *current < prior.duration:
					// A reset invalidates the old task activity history.
					delete(a.last, id)
				case !observedAt.After(prior.sampledAt):
					// Out-of-order / equal-time samples have no valid window.
					delete(a.last, id)
				default:
					delta := *current - prior.duration
					window := observedAt.Sub(prior.sampledAt).Seconds()
					info.TaskDeltaSeconds = &delta
					info.TaskSampleWindowSeconds = &window
					if delta > 0 {
						a.last[id] = observedAt
					}
				}
			}
			a.previous[id] = taskBaseline{duration: *current, epoch: info.MetricSessionEpoch, sampledAt: observedAt}
		}
		if t, ok := a.last[id]; ok {
			copy := t
			info.ObservedTaskActivityAt = &copy
			info.ObservedTaskActivitySource = ObservedTaskActivitySourceTaskDuration
		}
	}
	for id := range a.previous {
		if !seen[id] {
			delete(a.previous, id)
		}
	}
	for id := range a.last {
		if !seen[id] {
			delete(a.last, id)
		}
	}
}
