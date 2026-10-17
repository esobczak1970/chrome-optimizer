package browser

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWatchSamplerRetainsEpochAndReconcilesTargets(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := newWatchSampler(ctx, time.Second)
	defer w.close()
	samples := make(map[string]int)
	var mu sync.Mutex
	w.sampler = func(session *watchedTarget, tab Tab, _ time.Duration) TabInfo {
		mu.Lock()
		samples[tab.TargetID]++
		count := samples[tab.TargetID]
		mu.Unlock()
		return TabInfo{
			Tab:                 tab,
			MetricSessionEpoch:  session.epoch,
			TaskDurationSeconds: floatPtr(float64(count)),
		}
	}
	tabs := []Tab{{TargetID: "A", Title: "First"}, {TargetID: "B", Title: "Second"}}
	first := w.sample(tabs)
	if len(first) != 2 || first[0].MetricSessionEpoch == 0 || first[1].MetricSessionEpoch == 0 {
		t.Fatalf("missing persistent epochs: %+v", first)
	}
	second := w.sample(tabs)
	if second[0].MetricSessionEpoch != first[0].MetricSessionEpoch ||
		second[1].MetricSessionEpoch != first[1].MetricSessionEpoch {
		t.Fatalf("refresh reattached existing targets: first=%+v second=%+v", first, second)
	}
	if *second[0].TaskDurationSeconds <= *first[0].TaskDurationSeconds {
		t.Fatal("same-session task counters failed to advance")
	}
	// Closing A detaches it; opening C creates a fresh epoch while B
	// continues on its existing CDP session.
	next := w.sample([]Tab{{TargetID: "B", Title: "Updated"}, {TargetID: "C"}})
	if _, exists := w.sessions["A"]; exists {
		t.Fatal("closed target still has an attached monitoring session")
	}
	if len(next) != 2 || next[0].MetricSessionEpoch != first[1].MetricSessionEpoch ||
		next[1].MetricSessionEpoch == first[0].MetricSessionEpoch {
		t.Fatalf("target reconciliation lost continuity: %+v", next)
	}
	if next[0].Title != "Updated" {
		t.Fatalf("title update was not reflected: %+v", next[0])
	}
}

func TestWatchSamplerFailureTriggersFreshEpoch(t *testing.T) {
	w := newWatchSampler(context.Background(), time.Second)
	defer w.close()
	fail := false
	w.sampler = func(s *watchedTarget, tab Tab, _ time.Duration) TabInfo {
		info := TabInfo{Tab: tab, MetricSessionEpoch: s.epoch, TaskDurationSeconds: floatPtr(1)}
		if fail {
			info.MetricsError = "page target vanished"
		}
		return info
	}
	tabs := []Tab{{TargetID: "A"}}
	first := w.sample(tabs)
	fail = true
	failed := w.sample(tabs)
	if failed[0].MetricsError == "" || len(w.sessions) != 0 {
		t.Fatalf("failed target must be detached and flagged: %+v", failed)
	}
	fail = false
	reconnected := w.sample(tabs)
	if reconnected[0].MetricSessionEpoch == first[0].MetricSessionEpoch {
		t.Fatal("reconnected session reused stale task baseline")
	}
}

func TestWatchSamplerLimitsPersistentSessions(t *testing.T) {
	w := newWatchSampler(context.Background(), time.Second)
	defer w.close()
	w.sampler = func(s *watchedTarget, tab Tab, _ time.Duration) TabInfo {
		return TabInfo{Tab: tab, MetricSessionEpoch: s.epoch, TaskDurationSeconds: floatPtr(1)}
	}
	var tabs []Tab
	for i := range maxWatchSessions + 1 {
		tabs = append(tabs, Tab{TargetID: fmt.Sprintf("target-%03d", i)})
	}
	infos := w.sample(tabs)
	if len(w.sessions) != maxWatchSessions || len(infos) != len(tabs) {
		t.Fatalf("watch retained %d sessions for %d targets", len(w.sessions), len(infos))
	}
	if !strings.Contains(infos[len(infos)-1].MetricsError, "limit") {
		t.Fatalf("limit should display status instead of fabricated metrics: %+v", infos[len(infos)-1])
	}
}

func TestWatchRejectsBadArgumentsWithoutChrome(t *testing.T) {
	client, err := NewClient("http://127.0.0.1:9222", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for _, interval := range []time.Duration{-time.Second, 0, time.Millisecond, time.Hour} {
		err := client.Watch(context.Background(), interval, func([]TabInfo) error { return nil })
		if err == nil {
			t.Errorf("invalid watch interval %s accepted", interval)
		}
	}
	if err := client.Watch(context.Background(), time.Second, nil); err == nil {
		t.Fatal("nil watch callback was accepted")
	}
}

func TestWatchNoPageFailsWithoutCreatingTab(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch r.URL.Path {
		case "/json/version":
			fmt.Fprintf(w, `{"webSocketDebuggerUrl":"ws://%s/devtools/browser/id"}`, r.Host)
		case "/json/list":
			fmt.Fprint(w, `[{"id":"worker","type":"service_worker"}]`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	err = client.Watch(context.Background(), time.Second, func([]TabInfo) error {
		t.Fatal("watch should not dispatch without an existing page")
		return errors.New("unexpected callback")
	})
	if err == nil || !strings.Contains(err.Error(), "no page") {
		t.Fatalf("expected actionable no-page error: %v", err)
	}
	if requests != 2 {
		t.Fatalf("watch made %d requests, expected only two", requests)
	}
}
