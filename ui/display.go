// Package ui renders the read-only terminal view. The desktop interface will
// share the browser model but will not be required by CLI commands.
package ui

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"unicode"

	"github.com/esobczak1970/chrome-optimizer/browser"
)

// Render lists basic tab metadata. CPU, memory and last-used are deliberately
// omitted until CDP sampling/observation in Issues #2 and #3 is implemented.
func Render(w io.Writer, input []browser.Tab, order string) error {
	tabs := append([]browser.Tab(nil), input...)
	sort.SliceStable(tabs, func(i, j int) bool {
		var a, b string
		switch order {
		case "title":
			a, b = tabs[i].Title, tabs[j].Title
		case "id":
			a, b = tabs[i].TargetID, tabs[j].TargetID
		default:
			a, b = tabs[i].Domain, tabs[j].Domain
		}
		if strings.EqualFold(a, b) {
			return tabs[i].TargetID < tabs[j].TargetID
		}
		return strings.ToLower(a) < strings.ToLower(b)
	})
	if len(tabs) == 0 {
		_, err := fmt.Fprintln(w, "No existing Chrome page targets found.")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "TARGET ID\tDOMAIN\tTITLE\tURL"); err != nil {
		return err
	}
	for _, tab := range tabs {
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			clean(tab.TargetID), clean(tab.Domain), clean(tab.Title), clean(tab.URL)); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}
