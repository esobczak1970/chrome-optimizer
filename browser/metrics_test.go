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


func TestTaskActivityTrackerMeasuresOnlyValidSampleWindows(t *testing.T) {
	tracker := NewTaskActivityTracker()
	base := time.Date(2026, 10, 23, 15, 0, 0, 0, time.UTC)
	makeSample := func(seconds float64, at time.Time) []TabInfo {
		return []TabInfo{{
			Tab:                 Tab{TargetID: "tab-1"},
			MetricSessionEpoch:  9,
			TaskDurationSeconds: floatPtr(seconds),
			SampledAt:           at,
		}}
	}
	first := makeSample(2.25, base)
	tracker.Observe(first, base.Add(time.Second))
	if first[0].TaskDeltaSeconds != nil || first[0].TaskSampleWindowSeconds != nil {
		t.Fatal("first reading must not invent a sampling window")
	}

	second := makeSample(3.75, base.Add(2*time.Second))
	tracker.Observe(second, base.Add(5*time.Second))
	if second[0].TaskDeltaSeconds == nil || *second[0].TaskDeltaSeconds != 1.5 ||
		second[0].TaskSampleWindowSeconds == nil || *second[0].TaskSampleWindowSeconds != 2 {
		t.Fatalf("incorrect measured task-time delta or sample window: %+v", second[0])
	}
	if second[0].ObservedTaskActivityAt == nil || !second[0].ObservedTaskActivityAt.Equal(base.Add(2*time.Second)) {
		t.Fatalf("activity timestamp must be the CDP sample time, not callback time: %+v", second[0])
	}

	steady := makeSample(3.75, base.Add(4*time.Second))
	tracker.Observe(steady, base.Add(6*time.Second))
	if steady[0].TaskDeltaSeconds == nil || *steady[0].TaskDeltaSeconds != 0 ||
		steady[0].TaskSampleWindowSeconds == nil || *steady[0].TaskSampleWindowSeconds != 2 {
		t.Fatalf("unchanged counters must produce a measured zero (not unknown): %+v", steady[0])
	}
	if steady[0].ObservedTaskActivityAt == nil || !steady[0].ObservedTaskActivityAt.Equal(base.Add(2*time.Second)) {
		t.Fatal("unchanged counter must preserve the actual last observed increase")
	}

	outOfOrder := makeSample(4, base.Add(time.Second))
	tracker.Observe(outOfOrder, base.Add(7*time.Second))
	if outOfOrder[0].TaskDeltaSeconds != nil || outOfOrder[0].TaskSampleWindowSeconds != nil ||
		outOfOrder[0].ObservedTaskActivityAt != nil {
		t.Fatalf("out-of-order samples must discard windows/activity: %+v", outOfOrder[0])
	}
	recovered := makeSample(4.25, base.Add(5*time.Second))
	tracker.Observe(recovered, base.Add(8*time.Second))
	if recovered[0].TaskDeltaSeconds == nil || *recovered[0].TaskDeltaSeconds != 0.25 ||
		recovered[0].TaskSampleWindowSeconds == nil || *recovered[0].TaskSampleWindowSeconds != 4 {
		t.Fatalf("post-reset sample should have a new valid window: %+v", recovered[0])
	}
}

func TestTaskActivityTrackerRejectsInvalidCountersAndFailedSamples(t *testing.T) {
	cases := []struct {
		name string
		task *float64
		err  string
	}{
		{name: "missing"},
		{name: "not-a-number", task: floatPtr(math.NaN())},
		{name: "infinite", task: floatPtr(math.Inf(1))},
		{name: "negative", task: floatPtr(-0.1)},
		{name: "failed-reading", task: floatPtr(100), err: "target detached"},
	}
	base := time.Date(2026, 10, 23, 16, 0, 0, 0, time.UTC)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tracker := NewTaskActivityTracker()
			tracker.Observe([]TabInfo{{Tab: Tab{TargetID: "a"}, TaskDurationSeconds: floatPtr(1)}}, base)
			increase := []TabInfo{{Tab: Tab{TargetID: "a"}, TaskDurationSeconds: floatPtr(2)}}
			tracker.Observe(increase, base.Add(time.Second))
			if increase[0].ObservedTaskActivityAt == nil {
				t.Fatal("expected initial valid activity")
			}
			bad := []TabInfo{{
				Tab: Tab{TargetID: "a"}, TaskDurationSeconds: tc.task,
				MetricsError: tc.err,
			}}
			tracker.Observe(bad, base.Add(2*time.Second))
			if bad[0].TaskDeltaSeconds != nil || bad[0].TaskSampleWindowSeconds != nil ||
				bad[0].ObservedTaskActivityAt != nil {
				t.Fatalf("bad sample must not retain activity or delta: %+v", bad[0])
			}
			next := []TabInfo{{Tab: Tab{TargetID: "a"}, TaskDurationSeconds: floatPtr(9)}}
			tracker.Observe(next, base.Add(3*time.Second))
			if next[0].TaskDeltaSeconds != nil || next[0].ObservedTaskActivityAt != nil {
				t.Fatal("reading after invalid sample must be a fresh baseline")
			}
		})
	}
}
