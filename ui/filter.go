package ui

import (
	"strings"

	"github.com/esobczak1970/chrome-optimizer/browser"
)

// FilterInfos returns a new slice preserving tab order and input immutability.
// The same lightweight, stateless filter can be reused by the future desktop
// interface. Nothing about a query or browsing history is persisted to disk.
func FilterInfos(input []browser.TabInfo, query string) []browser.TabInfo {
	needle := strings.ToLower(strings.TrimSpace(query))
	out := make([]browser.TabInfo, 0, len(input))
	for _, tab := range input {
		if needle == "" ||
			strings.Contains(strings.ToLower(tab.TargetID), needle) ||
			strings.Contains(strings.ToLower(tab.Domain), needle) ||
			strings.Contains(strings.ToLower(tab.Title), needle) ||
			strings.Contains(strings.ToLower(tab.URL), needle) {
			out = append(out, tab)
		}
	}
	return out
}
