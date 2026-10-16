package browser

import (
	"context"
	"fmt"
	"log"

	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// TabInfo holds our mapped data
type TabInfo struct {
	TargetID string
	Title    string
	URL      string
	Type     string
}

// GetOpenTabs connects to the local Chrome instance and retrieves all targets
func GetOpenTabs() ([]TabInfo, error) {
	// 1. Connect to the running Chrome instance on port 9222
	allocatorCtx, cancel := chromedp.NewRemoteAllocator(context.Background(), "ws://localhost:9222/")
	defer cancel()

	// 2. Create a context for our actions
	ctx, cancelCtx := chromedp.NewContext(allocatorCtx)
	defer cancelCtx()

	var tabs []TabInfo

	// 3. Execute the CDP command to get all targets
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		targets, err := target.GetTargets().Do(ctx)
		if err != nil {
			return err
		}

		for _, t := range targets {
			// We only care about actual web pages, not background workers or extensions
			if t.Type == "page" {
				tabs = append(tabs, TabInfo{
					TargetID: string(t.TargetID),
					Title:    t.Title,
					URL:      t.URL,
					Type:     t.Type,
				})
			}
		}
		return nil
	}))

	if err != nil {
		return nil, fmt.Errorf("failed to get targets: %v", err)
	}

	return tabs, nil
}
