package ui

import (
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/esobczak1970/chrome-optimizer/browser"
)

// RenderMetricsAuto uses the current terminal width on every call (including
// watch refreshes). Redirected output retains the original stable text format.
func RenderMetricsAuto(w io.Writer, input []browser.TabInfo, key string, desc, compact bool) error {
	width := terminalColumns(w)
	if width == 0 {
		if compact {
			return RenderMetricsCompact(w, input, key, desc)
		}
		return RenderMetrics(w, input, key, desc)
	}
	return RenderMetricsWidth(w, input, key, desc, compact, width)
}

// RenderMetricsWidth is a deterministic, width-bounded terminal view. It never
// mutates tab data or mistakes missing metrics for zero. Narrow layouts switch
// to per-tab records rather than emitting a wider-than-terminal table.
func RenderMetricsWidth(w io.Writer, input []browser.TabInfo, key string, desc, compact bool, width int) error {
	if width <= 0 {
		return fmt.Errorf("terminal width must be positive: %d", width)
	}
	if len(input) == 0 {
		_, err := fmt.Fprintln(w, fitTerminal("No existing Chrome page targets found.", width))
		return err
	}
	sorted := SortInfos(input, key, desc)
	if width < 63 {
		for _, tab := range sorted {
			heap, task, activity, status := terminalValues(tab)
			lines := []string{
				"ID " + tab.TargetID,
				"TITLE " + tab.Title,
				fmt.Sprintf("JS HEAP MiB %s  TASK s %s", heap, task),
				fmt.Sprintf("DOMAIN %s  ACTIVITY %s", tab.Domain, activity),
				"STATUS " + status,
			}
			for _, line := range lines {
				if _, err := fmt.Fprintln(w, strings.TrimRight(fitTerminal(line, width), " ")); err != nil {
					return err
				}
			}
		}
		return nil
	}

	type column struct {
		field, header string
		width         int
	}
	idW, domainW, statusW := 8, 13, 10
	if width >= 95 {
		idW, domainW, statusW = 10, 18, 15
	}
	if width >= 145 {
		domainW, statusW = 24, 23
	}
	cols := []column{{"id", "ID", idW}, {"heap", "JS HEAP MiB", 11}, {"task", "TASK s", 8}, {"title", "TITLE", 10}, {"status", "STATUS", statusW}}
	if width >= 81 {
		cols = append(cols[:1], append([]column{{"domain", "DOMAIN", domainW}}, cols[1:]...)...)
	}
	if width >= 106 {
		index := 3
		if width >= 81 {
			index++
		}
		cols = append(cols[:index], append([]column{{"activity", "ACTIVITY", 8}}, cols[index:]...)...)
	}
	if !compact && width >= 170 {
		cols = append(cols[:len(cols)-1], append([]column{{"url", "URL", 32}}, cols[len(cols)-1:]...)...)
	}
	fixed := 2 * (len(cols) - 1)
	titleIndex := -1
	for i, c := range cols {
		if c.field == "title" {
			titleIndex = i
		} else {
			fixed += c.width
		}
	}
	cols[titleIndex].width = width - fixed
	if cols[titleIndex].width < 8 {
		return fmt.Errorf("terminal layout cannot fit in %d columns", width)
	}
	// A tabwriter assumes one terminal cell per rune; explicit padding keeps
	// CJK and wide emoji aligned without introducing ANSI control sequences.
	printRow := func(values map[string]string) error {
		var row strings.Builder
		for i, c := range cols {
			if i != 0 {
				row.WriteString("  ")
			}
			row.WriteString(fitTerminal(values[c.field], c.width))
		}
		_, err := fmt.Fprintln(w, strings.TrimRight(row.String(), " "))
		return err
	}
	headings := make(map[string]string, len(cols))
	for _, c := range cols {
		headings[c.field] = c.header
	}
	if err := printRow(headings); err != nil {
		return err
	}
	for _, tab := range sorted {
		heap, task, activity, status := terminalValues(tab)
		values := map[string]string{
			"id": tab.TargetID, "domain": tab.Domain, "heap": heap,
			"task": task, "activity": activity, "title": tab.Title,
			"url": tab.URL, "status": status,
		}
		if err := printRow(values); err != nil {
			return err
		}
	}
	return nil
}

func terminalValues(tab browser.TabInfo) (heap, task, activity, status string) {
	heap, task, activity = "-", "-", "-"
	if tab.JSHeapUsedBytes != nil {
		heap = fmt.Sprintf("%.1f", float64(*tab.JSHeapUsedBytes)/(1024*1024))
	}
	if validTaskDuration(tab.TaskDurationSeconds) {
		task = fmt.Sprintf("%.3f", *tab.TaskDurationSeconds)
		if *tab.TaskDurationSeconds > 0 && *tab.TaskDurationSeconds < 0.01 {
			task = fmt.Sprintf("%.6f", *tab.TaskDurationSeconds)
		}
	}
	if tab.ObservedTaskActivityAt != nil {
		activity = tab.ObservedTaskActivityAt.Local().Format("15:04:05")
	}
	status = "OK"
	if tab.MetricsError != "" {
		status = tab.MetricsError
	} else if tab.JSHeapUsedBytes == nil || !validTaskDuration(tab.TaskDurationSeconds) {
		status = "partial / unsupported"
	}
	return
}

// runeColumns estimates common terminal cell widths for CJK text and emoji.
// The source strings are sanitized first so escape/control text cannot spoof
// another row or inject terminal formatting.
func runeColumns(r rune) int {
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) {
		return 0
	}
	if r >= 0x1100 && (r <= 0x115f || r == 0x2329 || r == 0x232a ||
		(r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) ||
		(r >= 0xac00 && r <= 0xd7a3) || (r >= 0xf900 && r <= 0xfaff) ||
		(r >= 0xfe10 && r <= 0xfe19) || (r >= 0xfe30 && r <= 0xfe6f) ||
		(r >= 0xff00 && r <= 0xff60) || (r >= 0xffe0 && r <= 0xffe6) ||
		(r >= 0x1f300 && r <= 0x1faff) || (r >= 0x20000 && r <= 0x3fffd)) {
		return 2
	}
	return 1
}

func fitTerminal(source string, width int) string {
	if width <= 0 {
		return ""
	}
	sanitized := clean(source)
	total := 0
	for _, r := range sanitized {
		total += runeColumns(r)
	}
	limit := width
	if total > width {
		limit--
	}
	var b strings.Builder
	used := 0
	for _, r := range sanitized {
		n := runeColumns(r)
		if used+n > limit {
			break
		}
		b.WriteRune(r)
		used += n
	}
	if total > width {
		b.WriteRune('…')
		used++
	}
	if used < width {
		b.WriteString(strings.Repeat(" ", width-used))
	}
	return b.String()
}
