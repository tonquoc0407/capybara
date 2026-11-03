package hub

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAlertMonitorSendsOnceUntilThresholdClears(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "alert.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	if err := st.WriteBatch(ctx, store.Batch{Source: "test", Spans: []store.Span{{
		ID: "s", RunID: "r", Kind: store.KindAgent, Name: "agent", Status: "error",
		StartedAt: at, EndedAt: at.Add(2 * time.Second),
	}}}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	m := alertMonitor{store: st, config: AlertConfig{Project: "default", WebhookURL: "https://example.test/alert",
		Window: 10 * time.Minute, Interval: time.Minute, MinErrors: 1},
		client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			body, _ := io.ReadAll(req.Body)
			if !strings.Contains(string(body), `"errors":1`) || !strings.Contains(string(body), `"violations":["errors"]`) {
				t.Errorf("wrong alert payload: %s", body)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header)}, nil
		})}}
	if sent, err := m.check(ctx, at.Add(time.Minute)); err != nil || !sent {
		t.Fatalf("first check: %v %v", sent, err)
	}
	if sent, err := m.check(ctx, at.Add(2*time.Minute)); err != nil || sent {
		t.Fatalf("duplicate check: %v %v", sent, err)
	}
	if sent, err := m.check(ctx, at.Add(20*time.Minute)); err != nil || sent {
		t.Fatalf("clear check: %v %v", sent, err)
	}
	if sent, err := m.check(ctx, at.Add(time.Minute)); err != nil || !sent || calls != 2 {
		t.Fatalf("rearm: %v %v, calls %d", sent, err, calls)
	}
}
