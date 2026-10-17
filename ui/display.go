// Package ui renders read-only terminal diagnostics, shared by the CLI and
// future native GUI without importing any desktop widget dependencies.
package ui

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"
	"unicode"

	"github.com/esobczak1970/chrome-optimizer/browser"
)

// SortInfos returns a new slice, never modifying a caller's browser snapshot.
// Unknown metrics always sort last, even in descending order. TargetID gives
// deterministic output when the selected measurements are equal.
func SortInfos(input []browser.TabInfo, key string, desc bool) []browser.TabInfo {
	out := slices.Clone(input)
	slices.SortStableFunc(out, func(a, b browser.TabInfo) int {
		var comparison int
		var missingA, missingB bool
		switch key {
		case "memory":
			missingA, missingB = a.JSHeapUsedBytes == nil, b.JSHeapUsedBytes == nil
			if !missingA && !missingB {
				comparison = cmp.Compare(*a.JSHeapUsedBytes, *b.JSHeapUsedBytes)
			}
		case "cpu":
			missingA, missingB = a.TaskDurationSeconds == nil, b.TaskDurationSeconds == nil
			if !missingA && !missingB {
				comparison = cmp.Compare(*a.TaskDurationSeconds, *b.TaskDurationSeconds)
			}
		case "activity":
			missingA, missingB = a.ObservedTaskActivityAt == nil, b.ObservedTaskActivityAt == nil
			if !missingA && !missingB {
				comparison = a.ObservedTaskActivityAt.Compare(*b.ObservedTaskActivityAt)
			}
		case "title":
			comparison = cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
		case "id":
			comparison = cmp.Compare(a.TargetID, b.TargetID)
		default:
			comparison = cmp.Compare(strings.ToLower(a.Domain), strings.ToLower(b.Domain))
		}
		if missingA != missingB {
			if missingA {
				return 1
			}
			return -1
		}
		if desc {
			comparison = -comparison
		}
		if comparison == 0 {
			return cmp.Compare(a.TargetID, b.TargetID)
		}
		return comparison
	})
	return out
}

// Render preserves the Phase 1 API while sharing the single metrics-aware
// renderer. Missing readings are displayed as unknown, never as a zero.
func Render(w io.Writer, input []browser.Tab, key string) error {
	infos := make([]browser.TabInfo, len(input))
	for i, tab := range input {
		infos[i] = browser.TabInfo{Tab: tab}
	}
	return RenderMetrics(w, infos, key, false)
}

// RenderMetrics clearly identifies the CDP quantities: JS heap bytes and
// cumulative task time are not total process RAM or instantaneous CPU percent.
func RenderMetrics(w io.Writer, input []browser.TabInfo, key string, desc bool) error {
	return renderMetrics(w, input, key, desc, false)
}

// RenderMetricsCompact keeps terminal output within more typical window widths.
// It omits potentially very long URL values but never changes any CDP data.
func RenderMetricsCompact(w io.Writer, input []browser.TabInfo, key string, desc bool) error {
	return renderMetrics(w, input, key, desc, true)
}

func renderMetrics(w io.Writer, input []browser.TabInfo, key string, desc, compact bool) error {
	if len(input) == 0 {
		_, err := fmt.Fprintln(w, "No existing Chrome page targets found.")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	header := "TARGET ID\tDOMAIN\tJS HEAP MiB\tTASK TIME s\tLAST TASK ACTIVITY\tTITLE\tURL\tSTATUS"
	if compact {
		header = "ID\tDOMAIN\tJS HEAP MiB\tTASK s\tACTIVITY\tTITLE\tSTATUS"
	}
	if _, err := fmt.Fprintln(tw, header); err != nil {
		return err
	}
	for _, tab := range SortInfos(input, key, desc) {
		memory, cpu, activity := "-", "-", "-"
		if tab.JSHeapUsedBytes != nil {
			memory = fmt.Sprintf("%.1f", float64(*tab.JSHeapUsedBytes)/(1024*1024))
		}
		if tab.TaskDurationSeconds != nil {
			cpu = fmt.Sprintf("%.3f", *tab.TaskDurationSeconds)
		}
		if tab.ObservedTaskActivityAt != nil {
			activity = tab.ObservedTaskActivityAt.Local().Format("15:04:05")
		}
		status := "OK"
		if tab.MetricsError != "" {
			status = shorten(clean(tab.MetricsError), 66)
		} else if tab.JSHeapUsedBytes == nil || tab.TaskDurationSeconds == nil {
			status = "partial / unsupported"
		}
		if compact {
			if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				shorten(clean(tab.TargetID), 10), shorten(clean(tab.Domain), 26),
				memory, cpu, activity, shorten(clean(tab.Title), 38),
				shorten(status, 40)); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			clean(tab.TargetID), clean(tab.Domain), memory, cpu, activity,
			shorten(clean(tab.Title), 70), clean(tab.URL), status); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func shorten(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}
