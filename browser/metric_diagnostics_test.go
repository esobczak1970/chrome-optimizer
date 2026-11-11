package browser

import (
	"context"
	"errors"
	"strings"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/performance"
)

func TestParseMetricsDetailedDistinguishesUnavailableAndInvalid(t *testing.T) {
	tests := []struct {
		name      string
		metrics   []*performance.Metric
		heap      *uint64
		task      *float64
		heapError string
		taskError string
	}{
		{
			name:      "missing both",
			heapError: "JSHeapUsedSize missing",
			taskError: "TaskDuration missing",
		},
		{
			name: "valid zero is not missing",
			metrics: []*performance.Metric{
				{Name: "JSHeapUsedSize", Value: 0},
				{Name: "TaskDuration", Value: 0},
			},
			heap: metricHeap(0),
			task: metricTask(0),
		},
		{
			name: "unknown metrics do not create fake values",
			metrics: []*performance.Metric{
				nil,
				{Name: "Other", Value: 100},
			},
			heapError: "JSHeapUsedSize missing",
			taskError: "TaskDuration missing",
		},
		{
			name: "invalid floating point",
			metrics: []*performance.Metric{
				{Name: "JSHeapUsedSize", Value: math.NaN()},
				{Name: "TaskDuration", Value: math.Inf(1)},
			},
			heapError: "JSHeapUsedSize invalid",
			taskError: "TaskDuration invalid",
		},
		{
			name: "fractional and negative",
			metrics: []*performance.Metric{
				{Name: "JSHeapUsedSize", Value: 1.5},
				{Name: "TaskDuration", Value: -1},
			},
			heapError: "JSHeapUsedSize invalid",
			taskError: "TaskDuration invalid",
		},
		{
			name: "heap uint64 overflow",
			metrics: []*performance.Metric{
				{Name: "JSHeapUsedSize", Value: math.Exp2(64)},
				{Name: "TaskDuration", Value: 12.5},
			},
			task:      metricTask(12.5),
			heapError: "JSHeapUsedSize invalid",
		},
		{
			name: "valid duplicates survive invalid ones",
			metrics: []*performance.Metric{
				{Name: "JSHeapUsedSize", Value: 1048576},
				{Name: "TaskDuration", Value: 2},
				{Name: "JSHeapUsedSize", Value: math.NaN()},
				{Name: "TaskDuration", Value: -5},
			},
			heap: metricHeap(1048576),
			task: metricTask(2),
		},
		{
			name: "valid values recover after earlier invalid ones",
			metrics: []*performance.Metric{
				{Name: "JSHeapUsedSize", Value: -1},
				{Name: "TaskDuration", Value: math.Inf(-1)},
				{Name: "JSHeapUsedSize", Value: 4096},
				{Name: "TaskDuration", Value: 0.125},
			},
			heap: metricHeap(4096),
			task: metricTask(0.125),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseMetricsDetailed(tc.metrics)
			if (got.heap == nil) != (tc.heap == nil) ||
				(got.heap != nil && *got.heap != *tc.heap) {
				t.Errorf("heap = %v, want %v", got.heap, tc.heap)
			}
			if (got.task == nil) != (tc.task == nil) ||
				(got.task != nil && *got.task != *tc.task) {
				t.Errorf("task = %v, want %v", got.task, tc.task)
			}
			if got.heapError != tc.heapError || got.taskError != tc.taskError {
				t.Errorf("errors = (%q, %q), want (%q, %q)",
					got.heapError, got.taskError, tc.heapError, tc.taskError)
			}
		})
	}
}

func metricHeap(value uint64) *uint64 { return &value }
func metricTask(value float64) *float64 { return &value }

func TestApplyPerformanceMetricsRetainsSourceAndPerFieldErrors(t *testing.T) {
	info := TabInfo{Tab: Tab{TargetID: "tab-a", Title: "First"}}
	applyPerformanceMetrics(&info, []*performance.Metric{
		{Name: "JSHeapUsedSize", Value: 8192},
		{Name: "TaskDuration", Value: math.NaN()},
	})
	if info.MetricSource != MetricSourcePerformance || info.JSHeapUsedBytes == nil ||
		*info.JSHeapUsedBytes != 8192 || info.JSHeapError != "" ||
		info.TaskDurationSeconds != nil || info.TaskDurationError != "TaskDuration invalid" {
		t.Fatalf("source/field errors not retained: %+v", info)
	}
	if info.ObservedTaskActivityAt != nil || info.ObservedTaskActivitySource != "" {
		t.Fatal("one CDP sample cannot establish past tab activity")
	}
	applyPerformanceMetrics(&info, []*performance.Metric{
		{Name: "JSHeapUsedSize", Value: 0},
		{Name: "TaskDuration", Value: 0},
	})
	if info.JSHeapError != "" || info.TaskDurationError != "" ||
		info.JSHeapUsedBytes == nil || info.TaskDurationSeconds == nil {
		t.Fatalf("recovered metrics should have valid zeros: %+v", info)
	}
}

func TestCollectConcurrentLimitsWorkersAndPreservesTargetIdentity(t *testing.T) {
	tabs := make([]Tab, 18)
	for i := range tabs {
		tabs[i] = Tab{TargetID: string(rune('a' + i)), Title: "before"}
	}
	var active, peak atomic.Int32
	out := collectConcurrent(context.Background(), tabs, func(_ context.Context, tab Tab) TabInfo {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			max := peak.Load()
			if current <= max || peak.CompareAndSwap(max, current) {
				break
			}
		}
		// Deliberately vary completion order; the authoritative input is
		// still the source of each result's target identity and metadata.
		time.Sleep(time.Duration(1+int(tab.TargetID[0])%5) * time.Millisecond)
		return TabInfo{
			Tab:             Tab{TargetID: "stale", Title: "after"},
			JSHeapUsedBytes: metricHeap(uint64(tab.TargetID[0])),
		}
	})
	if peak.Load() > maxConcurrentSamples {
		t.Fatalf("used %d concurrent samplers; limit is %d", peak.Load(), maxConcurrentSamples)
	}
	for i := range tabs {
		if out[i].TargetID != tabs[i].TargetID || out[i].Title != "before" ||
			out[i].JSHeapUsedBytes == nil || *out[i].JSHeapUsedBytes != uint64(tabs[i].TargetID[0]) {
			t.Fatalf("out-of-order result %d lost metric/identity join: %+v", i, out[i])
		}
	}
}

func TestObservedActivitySourceOnlyAppearsAfterValidCounterIncrease(t *testing.T) {
	tracker := NewTaskActivityTracker()
	start := time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC)
	first := []TabInfo{{Tab: Tab{TargetID: "a"}, TaskDurationSeconds: metricTask(1)}}
	tracker.Observe(first, start)
	if first[0].ObservedTaskActivitySource != "" || first[0].ObservedTaskActivityAt != nil {
		t.Fatal("first reading invented historical activity")
	}
	second := []TabInfo{{Tab: Tab{TargetID: "a"}, TaskDurationSeconds: metricTask(2)}}
	tracker.Observe(second, start.Add(time.Second))
	if second[0].ObservedTaskActivityAt == nil ||
		second[0].ObservedTaskActivitySource != ObservedTaskActivitySourceTaskDuration {
		t.Fatalf("valid observed task increase needs source metadata: %+v", second[0])
	}
	failed := []TabInfo{{Tab: Tab{TargetID: "a"}, MetricsError: "target vanished"}}
	tracker.Observe(failed, start.Add(2*time.Second))
	if failed[0].ObservedTaskActivityAt != nil || failed[0].ObservedTaskActivitySource != "" {
		t.Fatalf("failed sample retained stale activity provenance: %+v", failed[0])
	}
}

func TestReadOneShotMetricsOrdersCommandsAndCleansUpFailures(t *testing.T) {
	for _, tc := range []struct {
		name         string
		enableError  error
		readError    error
		wantSequence string
		wantError    error
	}{
		{name: "success", wantSequence: "enable,get,disable"},
		{
			name: "enable failed",
			enableError: errors.New("cannot enable"),
			wantSequence: "enable",
			wantError: errors.New("cannot enable"),
		},
		{
			name: "read failed",
			readError: errors.New("page closed"),
			wantSequence: "enable,get,disable",
			wantError: errors.New("page closed"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var operations []string
			commands := performanceCommands{
				enable: func(context.Context) error {
					operations = append(operations, "enable")
					return tc.enableError
				},
				read: func(context.Context) ([]*performance.Metric, error) {
					operations = append(operations, "get")
					if tc.readError != nil {
						return nil, tc.readError
					}
					return []*performance.Metric{
						{Name: "JSHeapUsedSize", Value: 1048576},
						{Name: "TaskDuration", Value: 0},
					}, nil
				},
				disable: func(context.Context) error {
					operations = append(operations, "disable")
					return nil
				},
			}
			info := TabInfo{Tab: Tab{TargetID: "same-target"}}
			err := readOneShotMetrics(context.Background(), &info, commands)
			if got := strings.Join(operations, ","); got != tc.wantSequence {
				t.Fatalf("Performance command sequence %q, want %q", got, tc.wantSequence)
			}
			if tc.wantError != nil {
				if err == nil || !strings.Contains(err.Error(), tc.wantError.Error()) {
					t.Fatalf("expected wrapped %q error, got %v", tc.wantError, err)
				}
				if info.MetricSource != "" || info.JSHeapUsedBytes != nil {
					t.Fatalf("failed capture fabricated readings: %+v", info)
				}
				return
			}
			if err != nil || info.MetricSource != MetricSourcePerformance || info.SampledAt.IsZero() ||
				info.JSHeapUsedBytes == nil || *info.JSHeapUsedBytes != 1048576 ||
				info.TaskDurationSeconds == nil || *info.TaskDurationSeconds != 0 {
				t.Fatalf("successful capture lost source or valid zero: %+v; err %v", info, err)
			}
		})
	}
}
