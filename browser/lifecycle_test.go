package browser

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSelectLifecycleTargetRequiresExactUniqueID(t *testing.T) {
	tabs := []Tab{
		{TargetID: "A123", URL: "https://example.com"},
		{TargetID: "A124", URL: "https://example.com"},
	}
	selected, err := selectLifecycleTarget(tabs, "A123")
	if err != nil || selected.TargetID != "A123" {
		t.Fatalf("exact ID selection failed: %v, %v", selected, err)
	}
	for _, input := range []string{"A12", "example.com", "a123", "", "NOTFOUND"} {
		if _, err := selectLifecycleTarget(tabs, input); err == nil {
			t.Errorf("unsafe selector %q unexpectedly accepted", input)
		}
	}
	if _, err := selectLifecycleTarget(append(tabs, tabs[0]), "A123"); err == nil {
		t.Fatal("duplicate target ID must fail closed")
	}
}

func TestApplyLifecycleGuard(t *testing.T) {
	tests := []struct {
		name       string
		action     LifecycleAction
		url        string
		visibility string
		inspectErr error
		wantErr    string
		wantRead   bool
		wantChange bool
	}{
		{"hidden http", Freeze, "http://example.com/", "hidden:http:", nil, "", true, true},
		{"hidden https", Freeze, "https://example.com/path", "hidden:https:", nil, "", true, true},
		{"visible page", Freeze, "https://example.com", "visible:https:", nil, "not confirmed hidden", true, false},
		{"visibility unknown", Freeze, "https://example.com", "", errors.New("target gone"), "cannot verify", true, false},
		{"hidden file blocked", Freeze, "file:///Users/test/file.html", "hidden:file:", nil, "http(s)", false, false},
		{"chrome internal blocked", Freeze, "chrome://newtab/", "hidden:chrome:", nil, "http(s)", false, false},
		{"extension blocked", Freeze, "chrome-extension://abc/extension.html", "hidden:chrome-extension:", nil, "http(s)", false, false},
		{"javascript blocked", Freeze, "javascript:alert(1)", "hidden:javascript:", nil, "http(s)", false, false},
		{"empty URL blocked", Freeze, "", "hidden:https:", nil, "http(s)", false, false},
		{"resume visible", Resume, "https://example.com", "", nil, "", false, true},
		{"resume blocked origin", Resume, "chrome://newtab/", "", nil, "", false, true},
		{"unknown action", LifecycleAction("close"), "https://example.com", "hidden:https:", nil, "unsupported", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reads := 0
			changes := 0
			err := applyLifecycle(context.Background(), Tab{TargetID: "EXACT", URL: tt.url}, tt.action,
				func(context.Context) (string, error) {
					reads++
					return tt.visibility, tt.inspectErr
				},
				func(_ context.Context, action LifecycleAction) error {
					changes++
					if action != tt.action {
						t.Fatalf("incorrect operation %q", action)
					}
					return nil
				},
			)
			if tt.wantErr == "" && err != nil || tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("err=%v, want substring %q", err, tt.wantErr)
			}
			if (reads > 0) != tt.wantRead || (changes > 0) != tt.wantChange {
				t.Fatalf("unexpected calls: inspect=%d, change=%d", reads, changes)
			}
		})
	}
}

func TestApplyLifecyclePropagatesCDPFailure(t *testing.T) {
	expected := errors.New("protocol rejected experimental command")
	err := applyLifecycle(context.Background(), Tab{URL: "https://example.com"}, Freeze,
		func(context.Context) (string, error) { return "hidden:https:", nil },
		func(context.Context, LifecycleAction) error { return expected },
	)
	if !errors.Is(err, expected) {
		t.Fatalf("protocol error lost: %v", err)
	}
}

func TestApplyLifecycleCancelNeverCallsCDP(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := applyLifecycle(ctx, Tab{URL: "https://example.com"}, Freeze,
		func(context.Context) (string, error) { called = true; return "hidden:https:", nil },
		func(context.Context, LifecycleAction) error { called = true; return nil },
	)
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("cancel must prevent CDP action: err=%v called=%v", err, called)
	}
}

func TestApplyLifecycleCancelAfterVisibilityCheck(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	changed := false
	err := applyLifecycle(ctx, Tab{URL: "https://example.com"}, Freeze,
		func(context.Context) (string, error) {
			cancel()
			return "hidden:https:", nil
		},
		func(context.Context, LifecycleAction) error { changed = true; return nil },
	)
	if !errors.Is(err, context.Canceled) || changed {
		t.Fatalf("canceled operation reached lifecycle transition: %v", err)
	}
}

func TestLifecycleActionValidationBeforeCDP(t *testing.T) {
	client, err := NewClient("http://127.0.0.1:9222", 3e9)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		id     string
		action LifecycleAction
	}{
		{"", Freeze},
		{"TARGET", LifecycleAction("stop")},
	} {
		if _, err := client.SetLifecycle(context.Background(), item.id, item.action); err == nil {
			t.Fatalf("expected local rejection of %+v before CDP", item)
		}
	}
}

func TestPickBootstrapNeverUsesSelectedLifecycleTab(t *testing.T) {
	pages := []pageResponse{
		{ID: "target", Type: "page"},
		{ID: "worker", Type: "service_worker"},
		{ID: "helper", Type: "page"},
	}
	if got := pickBootstrapTarget(pages, "target"); got != "helper" {
		t.Fatalf("selected target must not be used for Chrome bootstrap: %q", got)
	}
	if got := pickBootstrapTarget(pages[:2], "target"); got != "" {
		t.Fatalf("missing helper must be rejected: %q", got)
	}
	if got := pickBootstrapTarget(pages, ""); got != "target" {
		t.Fatalf("normal read-only discovery should work unchanged: %q", got)
	}
}

func TestWithTargetsAvoidFailsClosedWithoutHelper(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch r.URL.Path {
		case "/json/version":
			fmt.Fprintf(w, `{"webSocketDebuggerUrl":"ws://%s/devtools/browser/test"}`, r.Host)
		case "/json/list":
			fmt.Fprint(w, `[{"id":"target","type":"page"}]`)
		default:
			t.Fatalf("unexpected request for %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, 3e9)
	if err != nil {
		t.Fatal(err)
	}
	err = client.withTargetsAvoid(context.Background(), "target", func(context.Context, []Tab) error {
		t.Fatal("operation must stop without a separate helper page")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "another open page") {
		t.Fatalf("expected actionable helper-tab error, got %v", err)
	}
	if requests != 2 {
		t.Fatalf("expected HTTP discovery only, got %d requests", requests)
	}
}
