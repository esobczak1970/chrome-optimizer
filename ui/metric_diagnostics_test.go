package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/esobczak1970/chrome-optimizer/browser"
)

func TestMetricStatusDistinguishesPartialFieldFailures(t *testing.T) {
	info := browser.TabInfo{
		Tab:               browser.Tab{TargetID: "a", Title: "Example", Domain: "example.test"},
		JSHeapError:       "JSHeapUsedSize invalid",
		TaskDurationError: "TaskDuration missing",
		MetricSource:      browser.MetricSourcePerformance,
	}
	status := metricStatus(info)
	for _, want := range []string{"partial / unsupported", "JSHeapUsedSize invalid", "TaskDuration missing"} {
		if !strings.Contains(status, want) {
			t.Fatalf("field-specific failure %q missing from %q", want, status)
		}
	}
	var full bytes.Buffer
	if err := RenderMetrics(&full, []browser.TabInfo{info}, "domain", false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"JSHeapUsedSize invalid", "TaskDuration missing"} {
		if !strings.Contains(full.String(), want) {
			t.Fatalf("full terminal output hides %q: %q", want, full.String())
		}
	}

	var compact bytes.Buffer
	if err := RenderMetricsCompact(&compact, []browser.TabInfo{info}, "domain", false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compact.String(), "JSHeapUsedSize") ||
		!strings.Contains(compact.String(), "partial / unsupported") {
		t.Fatalf("compact view lost partial-failure status: %q", compact.String())
	}

	var narrow bytes.Buffer
	if err := RenderMetricsWidth(&narrow, []browser.TabInfo{info}, "domain", false, true, 60); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(narrow.String(), "JSHeapUsedSize") {
		t.Fatalf("width-aware view lost metric diagnostics: %q", narrow.String())
	}
}

func TestMetricStatusPreservesTransportErrorsAndValidZero(t *testing.T) {
	if got := metricStatus(browser.TabInfo{MetricsError: "target closed", JSHeapError: "JSHeapUsedSize missing"}); got != "target closed" {
		t.Fatalf("transport failure should take priority: %q", got)
	}
	zeroBytes := uint64(0)
	zeroDuration := float64(0)
	if got := metricStatus(browser.TabInfo{
		JSHeapUsedBytes: &zeroBytes, TaskDurationSeconds: &zeroDuration,
	}); got != "OK" {
		t.Fatalf("valid zero values should not be marked unknown: %q", got)
	}
	if got := metricStatus(browser.TabInfo{}); got != "partial / unsupported" {
		t.Fatalf("legacy absent-metric status must remain stable: %q", got)
	}
}
