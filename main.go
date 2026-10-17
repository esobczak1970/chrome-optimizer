package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
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
	watch := flags.Bool("watch", false, "resample periodically until interrupted")
	intervalFlag := flags.Duration("interval", 0, "watch refresh interval (1s to 1m)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
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
	tracker := browser.NewTaskActivityTracker()
	printSample := func() error {
		tabs, err := client.Collect(ctx)
		if err != nil {
			return err
		}
		tracker.Observe(tabs, time.Now())
		if *watch {
			if _, err := fmt.Fprintf(out, "\n%s | %d page tabs | JS heap is not total RAM; task time is not CPU%%\n",
				time.Now().Local().Format(time.RFC3339), len(tabs)); err != nil {
				return err
			}
		}
		return ui.RenderMetrics(out, tabs, cfg.DefaultSort, *desc)
	}
	if err := printSample(); err != nil {
		return err
	}
	if !*watch {
		return nil
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := printSample(); err != nil {
				if errors.Is(err, context.Canceled) {
					return nil
				}
				return err
			}
		}
	}
}
