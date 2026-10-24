package ui

import (
	"bytes"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/esobczak1970/chrome-optimizer/browser"
)

func TestSortInfosInvalidTaskDurationsAlwaysLast(t *testing.T) {
	zero := 0.0
	high := 12.5
	nan := math.NaN()
	infinite := math.Inf(1)
	negative := -1.0

	infos := []browser.TabInfo{
		{Tab: browser.Tab{TargetID: "nan"}, TaskDurationSeconds: &nan},
		{Tab: browser.Tab{TargetID: "high"}, TaskDurationSeconds: &high},
		{Tab: browser.Tab{TargetID: "missing"}},
		{Tab: browser.Tab{TargetID: "zero"}, TaskDurationSeconds: &zero},
		{Tab: browser.Tab{TargetID: "infinite"}, TaskDurationSeconds: &infinite},
		{Tab: browser.Tab{TargetID: "negative"}, TaskDurationSeconds: &negative},
	}

	for _, tc := range []struct {
		name string
		desc bool
		want []string
	}{
		{"ascending", false, []string{"zero", "high", "infinite", "missing", "nan", "negative"}},
		{"descending", true, []string{"high", "zero", "infinite", "missing", "nan", "negative"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := SortInfos(infos, "cpu", tc.desc)
			for i, wantID := range tc.want {
				if got[i].TargetID != wantID {
					t.Fatalf("entry %d = %q, want %q (full: %+v)", i, got[i].TargetID, wantID, got)
				}
			}
		})
	}
	if infos[0].TargetID != "nan" {
		t.Fatal("sorting modified the input")
	}
}

func TestRenderMetricsInvalidDurationAndUnicodeControl(t *testing.T) {
	bad := math.NaN()
	info := browser.TabInfo{
		Tab: browser.Tab{
			TargetID: "id",
			Domain:   "example.com",
			Title:    "trusted\u202eevil\u202c title",
		},
		TaskDurationSeconds: &bad,
	}
	for _, render := range []struct {
		name string
		fn   func(w io.Writer, infos []browser.TabInfo, key string, desc bool) error
	}{
		{"full", RenderMetrics},
		{"compact", RenderMetricsCompact},
	} {
		t.Run(render.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := render.fn(&output, []browser.TabInfo{info}, "cpu", false); err != nil {
				t.Fatal(err)
			}
			got := output.String()
			if strings.Contains(got, "NaN") || strings.Contains(got, "\u202e") || strings.Contains(got, "\u202c") {
				t.Fatalf("invalid number or bidi formatting leaked into terminal: %q", got)
			}
			if !strings.Contains(got, "partial / unsupported") {
				t.Fatalf("missing metric should be reported as unknown: %q", got)
			}
		})
	}
}
