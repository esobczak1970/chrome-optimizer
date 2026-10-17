package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/esobczak1970/chrome-optimizer/browser"
)

func u(v uint64) *uint64 { return &v }
func f(v float64) *float64 { return &v }

func TestSortInfosUnknownAlwaysLast(t *testing.T) {
	data := []browser.TabInfo{
		{Tab: browser.Tab{TargetID: "unknown", Domain: "z.test"}},
		{Tab: browser.Tab{TargetID: "low", Domain: "b.test"}, JSHeapUsedBytes: u(5), TaskDurationSeconds: f(1)},
		{Tab: browser.Tab{TargetID: "high", Domain: "a.test"}, JSHeapUsedBytes: u(20), TaskDurationSeconds: f(3)},
	}
	for _, test := range []struct {
		key  string
		desc bool
		want []string
	}{
		{"memory", false, []string{"low", "high", "unknown"}},
		{"memory", true, []string{"high", "low", "unknown"}},
		{"cpu", false, []string{"low", "high", "unknown"}},
		{"cpu", true, []string{"high", "low", "unknown"}},
		{"domain", false, []string{"high", "low", "unknown"}},
		{"domain", true, []string{"unknown", "low", "high"}},
	} {
		out := SortInfos(data, test.key, test.desc)
		for i, want := range test.want {
			if out[i].TargetID != want {
				t.Fatalf("sort %s descending=%v: result %d = %s, want %s", test.key, test.desc, i, out[i].TargetID, want)
			}
		}
	}
	if data[0].TargetID != "unknown" {
		t.Fatal("sorting mutated input")
	}
}

func TestActivitySortingAndTieBreak(t *testing.T) {
	now := time.Date(2026, 10, 16, 18, 0, 0, 0, time.UTC)
	then := now.Add(-time.Minute)
	data := []browser.TabInfo{
		{Tab: browser.Tab{TargetID: "c"}, ObservedTaskActivityAt: &now},
		{Tab: browser.Tab{TargetID: "z"}},
		{Tab: browser.Tab{TargetID: "b"}, ObservedTaskActivityAt: &then},
		{Tab: browser.Tab{TargetID: "a"}, ObservedTaskActivityAt: &now},
	}
	out := SortInfos(data, "activity", true)
	for i, want := range []string{"a", "c", "b", "z"} {
		if out[i].TargetID != want {
			t.Fatalf("activity order = %+v; expected %s at %d", out, want, i)
		}
	}
}

func TestRenderMetricsLabelsAndMissingValues(t *testing.T) {
	tab := browser.TabInfo{
		Tab: browser.Tab{TargetID: "a", Domain: "example.test", Title: "Hello\nWorld", URL: "https://example.test"},
		JSHeapUsedBytes: u(1048576),
		TaskDurationSeconds: f(2.5),
	}
	missing := browser.TabInfo{
		Tab: browser.Tab{TargetID: "b", Domain: "chrome:", Title: "New Tab", URL: "chrome://newtab/"},
		MetricsError: "not supported",
	}
	var out bytes.Buffer
	if err := RenderMetrics(&out, []browser.TabInfo{missing, tab}, "memory", true); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"JS HEAP MiB", "TASK TIME s", "LAST TASK ACTIVITY", "1.0", "2.500", "not supported", "Hello World"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in output %q", want, text)
		}
	}
	if strings.Contains(text, "Hello\nWorld") {
		t.Fatal("untrusted title inserted a new terminal row")
	}
}
