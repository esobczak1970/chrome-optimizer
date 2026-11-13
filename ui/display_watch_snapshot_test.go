package ui

import (
	"bytes"
	"testing"

	"github.com/esobczak1970/chrome-optimizer/browser"
)

func TestSnapshotMatchesTerminalRendererWithoutWriterWidthLoss(t *testing.T) {
	input := []browser.TabInfo{{TargetID: "a-long-id", Domain: "example.test", Title: "News 漢字", URL: "https://example.test/path"}}
	for _, compact := range []bool{false, true} {
		var original bytes.Buffer
		if err := RenderMetricsAuto(&original, input, "domain", false, compact); err != nil {
			t.Fatal(err)
		}
		snapshot, err := RenderMetricsSnapshot(&bytes.Buffer{}, input, "domain", false, compact)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot != original.String() {
			t.Fatalf("snapshot does not preserve non-TTY formatting, compact=%v", compact)
		}
	}
}

func TestRenderMetricsAtWidthMatchesWidthAwareRenderer(t *testing.T) {
	input := []browser.TabInfo{{TargetID: "tab-a", Domain: "example.test", Title: "Unicode 漢字", URL: "https://example.test"}}
	for _, width := range []int{35, 80, 145, 190} {
		var original bytes.Buffer
		if err := RenderMetricsWidth(&original, input, "domain", false, false, width); err != nil {
			t.Fatal(err)
		}
		var snap bytes.Buffer
		if err := renderMetricsAtWidth(&snap, input, "domain", false, false, width); err != nil {
			t.Fatal(err)
		}
		if snap.String() != original.String() {
			t.Fatalf("width %d changed in buffered rendering", width)
		}
	}
}
