package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/esobczak1970/chrome-optimizer/browser"
)

func TestWidthRenderFitsAndSanitizes(t *testing.T) {
	heap := uint64(2097152)
	task := 0.000416
	input := []browser.TabInfo{
		{Tab: browser.Tab{TargetID: "z0123456789abcdef", Domain: "long.example.test", Title: "News 🎣 漢字 with a very long title", URL: "https://long.example.test/private?secret=not-in-compact"}, JSHeapUsedBytes: &heap, TaskDurationSeconds: &task},
		{Tab: browser.Tab{TargetID: "a", Domain: "safe.test", Title: "Untrusted\x1b[31m\nnext", URL: "https://safe.test"}},
	}
	for _, width := range []int{35, 50, 63, 80, 81, 94, 106, 145, 170, 240} {
		for _, compact := range []bool{false, true} {
			var out bytes.Buffer
			if err := RenderMetricsWidth(&out, input, "domain", false, compact, width); err != nil {
				t.Fatalf("width=%d compact=%v: %v", width, compact, err)
			}
			text := out.String()
			if strings.Contains(text, "\x1b") || (strings.Contains(text, "secret=not-in-compact") && compact) {
				t.Fatalf("escaped/leaked width=%d compact=%v: %q", width, compact, text)
			}
			for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
				count := 0
				for _, r := range line {
					count += runeColumns(r)
				}
				if count > width {
					t.Fatalf("width=%d compact=%v line=%q visible=%d", width, compact, line, count)
				}
			}
			if !strings.Contains(text, "a") {
				t.Fatalf("missing tab at %d", width)
			}
		}
	}
}

func TestWidthRenderRetainsNonzeroMetricsAndUnknown(t *testing.T) {
	heap := uint64(2 * 1024 * 1024)
	task := 0.000416
	infos := []browser.TabInfo{
		{Tab: browser.Tab{TargetID: "a", Domain: "example.org", Title: "First"}, JSHeapUsedBytes: &heap, TaskDurationSeconds: &task},
		{Tab: browser.Tab{TargetID: "b", Domain: "example.net", Title: "Unknown"}},
	}
	var out bytes.Buffer
	if err := RenderMetricsWidth(&out, infos, "cpu", false, true, 105); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "0.000416") || !strings.Contains(out.String(), "2.0") || !strings.Contains(out.String(), "partial / unsu") {
		t.Fatalf("metric distinction lost: %q", out.String())
	}
	if infos[0].TargetID != "a" {
		t.Fatal("input mutated")
	}
}

func TestWidthRenderRejectsNonpositive(t *testing.T) {
	if err := RenderMetricsWidth(&bytes.Buffer{}, nil, "domain", false, false, 0); err == nil {
		t.Fatal("width zero accepted")
	}
}

func TestAutoRenderKeepsRedirectedOutputStable(t *testing.T) {
	info := browser.TabInfo{Tab: browser.Tab{
		TargetID: "a-long-identity", Domain: "example.test", Title: "Visible title",
		URL: "https://example.test/path?internal=not-private",
	}}
	var normal bytes.Buffer
	if err := RenderMetricsAuto(&normal, []browser.TabInfo{info}, "domain", false, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(normal.String(), info.URL) {
		t.Fatalf("non-TTY full output unexpectedly removed URL: %q", normal.String())
	}
	var compact bytes.Buffer
	if err := RenderMetricsAuto(&compact, []browser.TabInfo{info}, "domain", false, true); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(compact.String(), info.URL) {
		t.Fatalf("non-TTY compact output leaked full URL: %q", compact.String())
	}
}
