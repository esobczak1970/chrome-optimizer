package browser

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/performance"
)

func uintPtr(v uint64) *uint64    { return &v }
func floatPtr(v float64) *float64 { return &v }

func TestParseMetrics(t *testing.T) {
	heap, task := parseMetrics([]*performance.Metric{
		nil,
		{Name: "Other", Value: 17},
		{Name: "JSHeapUsedSize", Value: math.NaN()},
		{Name: "JSHeapUsedSize", Value: -1},
		{Name: "JSHeapUsedSize", Value: 1048576},
		{Name: "TaskDuration", Value: math.Inf(1)},
		{Name: "TaskDuration", Value: 12.5},
	})
	if heap == nil || *heap != 1048576 {
		t.Fatalf("unexpected heap: %v", heap)
	}
	if task == nil || *task != 12.5 {
		t.Fatalf("unexpected task duration: %v", task)
	}
	heap, task = parseMetrics([]*performance.Metric{{Name: "TaskDuration", Value: -1}})
	if heap != nil || task != nil {
		t.Fatalf("missing/invalid metrics must remain nil: %v, %v", heap, task)
	}
}

func TestCollectConcurrentPreservesTargetIdentityAndOrder(t *testing.T) {
	tabs := []Tab{
		{TargetID: "a", URL: "https://a.test"},
		{TargetID: "b", URL: "https://b.test"},
		{TargetID: "c", URL: "https://c.test"},
	}
	out := collectConcurrent(context.Background(), tabs, func(_ context.Context, tab Tab) TabInfo {
		if tab.TargetID == "b" {
			return TabInfo{MetricsError: errors.New("target vanished").Error()}
		}
		return TabInfo{JSHeapUsedBytes: uintPtr(20)}
	})
	if len(out) != len(tabs) {
		t.Fatalf("expected %d results, got %d", len(tabs), len(out))
	}
	for i := range tabs {
		if out[i].TargetID != tabs[i].TargetID || out[i].URL != tabs[i].URL {
			t.Fatalf("result %d lost target identity: %+v", i, out[i])
		}
	}
	if out[1].MetricsError != "target vanished" || out[0].JSHeapUsedBytes == nil || out[2].JSHeapUsedBytes == nil {
		t.Fatalf("one failed tab must not erase others: %+v", out)
	}
	if tabs[0].URL != "https://a.test" {
		t.Fatal("caller snapshot changed")
	}
}

func TestCollectConcurrentCanceledWithoutRunningSampler(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var called atomic.Bool
	out := collectConcurrent(ctx, []Tab{{TargetID: "a"}}, func(context.Context, Tab) TabInfo {
		called.Store(true)
		return TabInfo{}
	})
	if called.Load() {
		t.Fatal("canceled collection invoked its sampler")
	}
	if len(out) != 1 || out[0].MetricsError != "collection canceled" {
		t.Fatalf("expected canceled target with preserved identity, got %+v", out)
	}
}

func TestTaskActivityTracker(t *testing.T) {
	tracker := NewTaskActivityTracker()
	now := time.Date(2026, 10, 16, 20, 0, 0, 0, time.UTC)
	sample := []TabInfo{{Tab: Tab{TargetID: "a"}, TaskDurationSeconds: floatPtr(10)}}
	tracker.Observe(sample, now)
	if sample[0].ObservedTaskActivityAt != nil {
		t.Fatal("first measurement is not evidence of a user action")
	}
	sample = []TabInfo{{Tab: Tab{TargetID: "a"}, TaskDurationSeconds: floatPtr(10)}}
	tracker.Observe(sample, now.Add(time.Second))
	if sample[0].ObservedTaskActivityAt != nil {
		t.Fatal("unchanged counter is not activity")
	}
	sample = []TabInfo{{Tab: Tab{TargetID: "a"}, TaskDurationSeconds: floatPtr(11)}}
	tracker.Observe(sample, now.Add(2*time.Second))
	if sample[0].ObservedTaskActivityAt == nil || !sample[0].ObservedTaskActivityAt.Equal(now.Add(2*time.Second)) {
		t.Fatalf("expected observed execution timestamp: %+v", sample)
	}
	sample = []TabInfo{{Tab: Tab{TargetID: "a"}, TaskDurationSeconds: floatPtr(1)}}
	tracker.Observe(sample, now.Add(3*time.Second))
	if sample[0].ObservedTaskActivityAt != nil {
		t.Fatal("counter reset must clear stale activity")
	}
	tracker.Observe(nil, now.Add(4*time.Second))
	if len(tracker.previous) != 0 || len(tracker.last) != 0 {
		t.Fatal("closed tab retained after scan")
	}
}

func TestMissingMetricsResetComparisonBaseline(t *testing.T) {
	tracker := NewTaskActivityTracker()
	now := time.Now()
	tracker.Observe([]TabInfo{{Tab: Tab{TargetID: "x"}, TaskDurationSeconds: floatPtr(2)}}, now)
	tracker.Observe([]TabInfo{{Tab: Tab{TargetID: "x"}}}, now.Add(time.Second))
	sample := []TabInfo{{Tab: Tab{TargetID: "x"}, TaskDurationSeconds: floatPtr(4)}}
	tracker.Observe(sample, now.Add(2*time.Second))
	if sample[0].ObservedTaskActivityAt != nil {
		t.Fatal("a failed intermediate sample must not create invented activity")
	}
}

func TestTaskActivityTrackerDoesNotCompareDifferentCDPSessions(t *testing.T) {
	tracker := NewTaskActivityTracker()
	now := time.Date(2026, 10, 16, 21, 55, 0, 0, time.UTC)
	first := []TabInfo{{
		Tab:                 Tab{TargetID: "news"},
		MetricSessionEpoch:  10,
		TaskDurationSeconds: floatPtr(0.001),
	}}
	tracker.Observe(first, now)
	if first[0].ObservedTaskActivityAt != nil {
		t.Fatal("a first reading cannot establish activity")
	}
	second := []TabInfo{{
		Tab:                 Tab{TargetID: "news"},
		MetricSessionEpoch:  10,
		TaskDurationSeconds: floatPtr(0.125),
	}}
	tracker.Observe(second, now.Add(time.Second))
	if second[0].ObservedTaskActivityAt == nil {
		t.Fatal("task time increased within same epoch but activity was not observed")
	}
	// A new CDP session might begin at any counter value. Comparing it to
	// the prior session is invalid even if its value happens to increase.
	reconnected := []TabInfo{{
		Tab:                 Tab{TargetID: "news"},
		MetricSessionEpoch:  11,
		TaskDurationSeconds: floatPtr(0.501),
	}}
	tracker.Observe(reconnected, now.Add(2*time.Second))
	if reconnected[0].ObservedTaskActivityAt != nil {
		t.Fatal("new session must reset both baseline and last activity")
	}
	stable := []TabInfo{{
		Tab:                 Tab{TargetID: "news"},
		MetricSessionEpoch:  11,
		TaskDurationSeconds: floatPtr(0.501),
	}}
	tracker.Observe(stable, now.Add(3*time.Second))
	if stable[0].ObservedTaskActivityAt != nil {
		t.Fatal("unchanged task counter is not activity")
	}
	increased := []TabInfo{{
		Tab:                 Tab{TargetID: "news"},
		MetricSessionEpoch:  11,
		TaskDurationSeconds: floatPtr(0.55),
	}}
	tracker.Observe(increased, now.Add(4*time.Second))
	if increased[0].ObservedTaskActivityAt == nil {
		t.Fatal("activity after valid second sample was not detected")
	}
}
