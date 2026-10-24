package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/esobczak1970/chrome-optimizer/browser"
	"github.com/esobczak1970/chrome-optimizer/config"
	"github.com/esobczak1970/chrome-optimizer/ui"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		fmt.Fprintln(os.Stderr, "chrome-optimizer:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out, errOut io.Writer) error {
	flags := flag.NewFlagSet("chrome-optimizer", flag.ContinueOnError)
	flags.SetOutput(errOut)
	configFile := flags.String("config", "config.yaml", "YAML configuration path")
	endpoint := flags.String("endpoint", "", "override loopback CDP endpoint")
	sortBy := flags.String("sort", "", "domain, title, id, memory, cpu or activity")
	desc := flags.Bool("desc", false, "sort descending; unknown metrics always last")
	filter := flags.String("filter", "", "case-insensitive substring search over target ID, domain, title and URL")
	compact := flags.Bool("compact", false, "compact terminal view without full URLs")
	watch := flags.Bool("watch", false, "resample periodically until interrupted")
	intervalFlag := flags.Duration("interval", 0, "watch refresh interval (1s to 1m)")
	freezeID := flags.String("freeze", "", "request freezing an exact background HTTP(S) tab TargetID")
	resumeID := flags.String("resume", "", "request resuming an exact tab TargetID")
	confirmFreeze := flags.Bool("confirm-freeze", false, "explicitly consent to modifying the selected tab's lifecycle")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	// Visiting flags distinguishes an omitted action from --freeze= or
	// --resume= with a missing ID; never silently turn the latter into scan.
	var freezeRequested, resumeRequested bool
	flags.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "freeze":
			freezeRequested = true
		case "resume":
			resumeRequested = true
		}
	})
	if freezeRequested && *freezeID == "" {
		return errors.New("--freeze requires a complete TargetID")
	}
	if resumeRequested && *resumeID == "" {
		return errors.New("--resume requires a complete TargetID")
	}
	if freezeRequested && resumeRequested {
		return errors.New("--freeze and --resume are mutually exclusive")
	}
	if *freezeID != "" && !*confirmFreeze {
		return errors.New("freezing changes a tab: specify --confirm-freeze together with --freeze=EXACT_TARGET_ID")
	}
	if *confirmFreeze && *freezeID == "" {
		return errors.New("--confirm-freeze requires --freeze=EXACT_TARGET_ID")
	}
	if *watch && (freezeRequested || resumeRequested) {
		return errors.New("--watch is read-only; lifecycle actions must run as separate one-shot commands")
	}
	cfg, err := config.Load(*configFile)
	if err != nil {
		return err
	}
	if *endpoint != "" {
		cfg.CDPEndpoint = *endpoint
	}
	if *sortBy != "" {
		cfg.DefaultSort = *sortBy
	}
	if !config.ValidSort(cfg.DefaultSort) {
		return fmt.Errorf("unsupported sort %q: choose domain, title, id, memory, cpu, or activity", cfg.DefaultSort)
	}
	timeout, err := cfg.Duration()
	if err != nil {
		return err
	}
	interval, err := cfg.PollDuration()
	if err != nil {
		return err
	}
	if *intervalFlag != 0 {
		interval = *intervalFlag
	}
	if interval < time.Second || interval > time.Minute {
		return fmt.Errorf("interval must be between 1s and 1m, got %s", interval)
	}
	client, err := browser.NewClient(cfg.CDPEndpoint, timeout)
	if err != nil {
		return err
	}
	if *freezeID != "" || *resumeID != "" {
		action, targetID := browser.Freeze, *freezeID
		if *resumeID != "" {
			action, targetID = browser.Resume, *resumeID
		}
		selected, err := client.SetLifecycle(ctx, targetID, action)
		if err != nil {
			return err
		}
		state := "frozen"
		if action == browser.Resume {
			state = "active"
		}
		// Titles are controlled by web pages, so never echo them raw in an
		// action result where control characters could spoof terminal output.
		_, err = fmt.Fprintf(out, "Chrome acknowledged %s lifecycle request for TargetID %s. This does not prove memory was released or that the state persisted.\n",
			state, selected.TargetID)
		return err
	}
	tracker := browser.NewTaskActivityTracker()
	noMatchesReported := false
	renderSample := func(tabs []browser.TabInfo) error {
		tracker.Observe(tabs, time.Now())
		tabs = ui.FilterInfos(tabs, *filter)
		if strings.TrimSpace(*filter) != "" && len(tabs) == 0 {
			if *watch && noMatchesReported {
				return nil
			}
			noMatchesReported = true
			_, err := fmt.Fprintln(out, "No page targets match the filter.")
			return err
		}
		noMatchesReported = false
		if *watch {
			if _, err := fmt.Fprintf(out, "\n%s | %d matching tabs | JS heap is not total RAM; task time is not CPU%%\n",
				time.Now().Local().Format(time.RFC3339), len(tabs)); err != nil {
				return err
			}
		}
		return ui.RenderMetricsAuto(out, tabs, cfg.DefaultSort, *desc, *compact)
	}
	if *watch {
		// Unlike one-shot Collect, Watch retains CDP instrumentation so its
		// TaskDuration counters remain comparable across refreshes.
		return client.Watch(ctx, interval, renderSample)
	}
	tabs, err := client.Collect(ctx)
	if err != nil {
		return err
	}
	return renderSample(tabs)
}
