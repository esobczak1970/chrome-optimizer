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
	sortBy := flags.String("sort", "", "sort by domain, title, or id")
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
	if cfg.DefaultSort != "domain" && cfg.DefaultSort != "title" && cfg.DefaultSort != "id" {
		return fmt.Errorf("unsupported sort %q: choose domain, title, or id", cfg.DefaultSort)
	}
	timeout, err := cfg.Duration()
	if err != nil {
		return err
	}
	client, err := browser.NewClient(cfg.CDPEndpoint, timeout)
	if err != nil {
		return err
	}
	tabs, err := client.Discover(ctx)
	if err != nil {
		return err
	}
	return ui.Render(out, tabs, cfg.DefaultSort)
}
