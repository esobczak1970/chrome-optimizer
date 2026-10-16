package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/esobczak1970/chrome-optimizer/browser"
)

func TestRenderSortedAndSanitized(t *testing.T) {
	tabs := []browser.Tab{
		{TargetID: "z", Domain: "z.test", Title: "bad\ntitle", URL: "https://z.test"},
		{TargetID: "a", Domain: "a.test", Title: "Alpha", URL: "https://a.test"},
	}
	var out bytes.Buffer
	if err := Render(&out, tabs, "domain"); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if strings.Index(s, "a.test") > strings.Index(s, "z.test") {
		t.Fatalf("rows not sorted by domain: %q", s)
	}
	if strings.Contains(s, "bad\ntitle") {
		t.Fatalf("control characters not sanitized: %q", s)
	}
	if tabs[0].TargetID != "z" {
		t.Fatal("Render mutated caller's slice")
	}
}
