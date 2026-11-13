package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/esobczak1970/chrome-optimizer/browser"
)

func TestWatchSampleSkipsUnchangedFrames(t *testing.T) {
	heap := uint64(1 << 20)
	task := 0.5
	tabs := []browser.TabInfo{{
		TargetID: "tab-a", Title: "Page A", Domain: "example.test",
		URL: "https://example.test/a", JSHeapUsedBytes: &heap,
		TaskDurationSeconds: &task,
	}}
	var out bytes.Buffer
	previous := ""
	if err := renderWatchSample(&out, tabs, "domain", false, false, &previous); err != nil {
		t.Fatal(err)
	}
	first := out.String()
	if !strings.Contains(first, "Page A") || !strings.Contains(first, "1 matching tabs") {
		t.Fatalf("first frame missing details: %q", first)
	}
	if err := renderWatchSample(&out, tabs, "domain", false, false, &previous); err != nil {
		t.Fatal(err)
	}
	if out.String() != first {
		t.Fatal("unchanged browser sample caused a redundant repaint")
	}
	// A new raw counter that rounds to the same *visible* value should
	// not force a terminal redraw, unlike raw-struct comparison.
	task = 0.50001
	if err := renderWatchSample(&out, tabs, "domain", false, false, &previous); err != nil {
		t.Fatal(err)
	}
	if out.String() != first {
		t.Fatal("unobservably small metric change caused a repaint")
	}
	heap = 3 << 20
	if err := renderWatchSample(&out, tabs, "domain", false, false, &previous); err != nil {
		t.Fatal(err)
	}
	if out.Len() <= len(first) || !strings.Contains(out.String()[len(first):], "3.0") {
		t.Fatalf("visible resource change did not repaint: %q", out.String())
	}
}

func TestWatchSampleReprintsForSortAndCompactChanges(t *testing.T) {
	tabs := []browser.TabInfo{{TargetID: "a-long-target", Domain: "example.test", Title: "Title", URL: "https://example.test/private?q=secret"}}
	var out bytes.Buffer
	previous := ""
	if err := renderWatchSample(&out, tabs, "domain", false, false, &previous); err != nil {
		t.Fatal(err)
	}
	before := out.Len()
	if err := renderWatchSample(&out, tabs, "domain", false, true, &previous); err != nil {
		t.Fatal(err)
	}
	if out.Len() <= before {
		t.Fatal("compact display change did not repaint")
	}
	if strings.Contains(out.String()[before:], "secret") {
		t.Fatal("compact snapshot exposed a URL query value")
	}
}
