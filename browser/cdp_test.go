package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDomain(t *testing.T) {
	tests := map[string]string{
		"https://Example.COM:443/path?a=b": "example.com",
		"https://[::1]:9222/x":         "::1",
		"about:blank":                 "about:",
		"chrome://settings/privacy":   "chrome:",
		"file:///Users/test/doc.html":  "file:",
		"":                            "-",
		"relative/path":               "(relative)",
		"%":                           "(invalid)",
	}
	for raw, want := range tests {
		if got := Domain(raw); got != want {
			t.Errorf("Domain(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestEndpointRejectsUnsafeHosts(t *testing.T) {
	for _, endpoint := range []string{
		"http://8.8.8.8:9222",
		"https://127.0.0.1:9222",
		"http://example.com:9222",
		"http://127.0.0.1:9222/json/list",
		"http://me:pw@127.0.0.1:9222",
		"http://127.0.0.1",
	} {
		t.Run(endpoint, func(t *testing.T) {
			if _, err := NewClient(endpoint, time.Second); err == nil {
				t.Fatal("expected endpoint rejection")
			}
		})
	}
}

func TestDiscoverNoExistingPageDoesNotCreateOne(t *testing.T) {
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		switch r.URL.Path {
		case "/json/version":
			fmt.Fprintf(w, `{"webSocketDebuggerUrl":"ws://%s/devtools/browser/id"}`, r.Host)
		case "/json/list":
			fmt.Fprint(w, `[{"id":"worker","type":"service_worker"}]`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	tabs, err := client.Discover(context.Background())
	if err != nil || len(tabs) != 0 {
		t.Fatalf("tabs %v, err %v", tabs, err)
	}
	if count != 2 {
		t.Fatalf("discovery made %d requests; expected only two", count)
	}
}

func TestDiscoverRejectsRemoteWebsocket(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"webSocketDebuggerUrl":"ws://198.51.100.7:9222/devtools/browser/id"}`)
	}))
	defer server.Close()
	client, err := NewClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Discover(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "non-loopback") {
		t.Fatalf("expected refusal of remote WebSocket, got %v", err)
	}
}

func TestDiscoveryDoesNotFollowRedirect(t *testing.T) {
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("unexpected request to redirected server")
	}))
	defer redirectTarget.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL, http.StatusFound)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Discover(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("expected redirect refusal, got %v", err)
	}
}

func TestDiscoverRequiresBrowserWebSocketPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"webSocketDebuggerUrl":"ws://%s/devtools/page/other"}`, r.Host)
	}))
	defer server.Close()
	client, err := NewClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Discover(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "WebSocket") {
		t.Fatalf("expected non-browser WS path refusal, got %v", err)
	}
}
