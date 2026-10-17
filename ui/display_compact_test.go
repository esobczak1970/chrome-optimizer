package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/esobczak1970/chrome-optimizer/browser"
)

func TestRenderCompactTruncatesLongFieldsAndOmitsURL(t *testing.T) {
	now := time.Date(2026, 10, 16, 20, 0, 0, 0, time.UTC)
	heap := uint64(2 * 1024 * 1024)
	duration := 7.5
	tab := browser.TabInfo{
		Tab: browser.Tab{
			TargetID: "0123456789ABCDEF9876543210ABCDEF",
			Domain:   "verylongsubdomain.example.com",
			Title:    "An extremely long news article title with 🎣 emoji and even more content",
			URL:      "https://verylongsubdomain.example.com/private/path?token=never-echo-in-compact",
		},
		JSHeapUsedBytes:        &heap,
		TaskDurationSeconds:    &duration,
		ObservedTaskActivityAt: &now,
	}
	var output bytes.Buffer
	if err := RenderMetricsCompact(&output, []browser.TabInfo{tab}, "memory", true); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, want := range []string{"ID", "DOMAIN", "JS HEAP MiB", "TASK s", "ACTIVITY", "TITLE", "STATUS", "2.0", "7.500", "012345678", "…"} {
		if !strings.Contains(text, want) {
			t.Errorf("compact table missing %q in %q", want, text)
		}
	}
	for _, avoid := range []string{"never-echo-in-compact", "0123456789ABCDEF9876543210ABCDEF", "even more content"} {
		if strings.Contains(text, avoid) {
			t.Errorf("compact table leaked long field %q", avoid)
		}
	}
	if tab.URL != "https://verylongsubdomain.example.com/private/path?token=never-echo-in-compact" {
		t.Fatal("renderer changed caller input")
	}
}

func TestRenderCompactMissingMetrics(t *testing.T) {
	var output bytes.Buffer
	if err := RenderMetricsCompact(&output, []browser.TabInfo{{Tab: browser.Tab{
		TargetID: "A", Domain: "chrome:", Title: "New Tab", URL: "chrome://newtab/",
	}}}, "memory", true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "partial / unsupported") {
		t.Fatalf("unknown values must remain distinguished: %q", output.String())
	}
}
