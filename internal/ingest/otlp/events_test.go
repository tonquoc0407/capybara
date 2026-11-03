package otlp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEventHandlerLifecycleAndErrors(t *testing.T) {
	r := New(openTemp(t), true)
	request := func(body string, remote string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/events", strings.NewReader(body))
		req.RemoteAddr = remote
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.handler().ServeHTTP(w, req)
		return w.Code
	}
	end := `{"event_id":"e2","type":"span.end","run_id":"r","span_id":"s","time":"2026-01-01T00:00:01Z","status":"ok"}`
	start := `{"event_id":"e1","type":"span.start","run_id":"r","span_id":"s","kind":"llm","name":"chat","time":"2026-01-01T00:00:00Z"}`
	for _, tc := range []struct {
		body, remote string
		status       int
	}{
		{end, "127.0.0.1:1", 409},
		{start, "192.0.2.1:1", 403},
		{start, "127.0.0.1:1", 201},
		{start, "127.0.0.1:1", 200},
		{`{"event_id":"early","type":"span.end","run_id":"r","span_id":"s","time":"2025-01-01T00:00:00Z","status":"ok"}`, "127.0.0.1:1", 400},
		{end, "127.0.0.1:1", 201},
		{`{"unknown":true}`, "127.0.0.1:1", 400},
		{`{"event_id":"bad-cost","type":"span.update","run_id":"r","span_id":"s","time":"2026-01-01T00:00:02Z","cost_usd":-1}`, "127.0.0.1:1", 400},
		{strings.Repeat(" ", maxEventBody+1), "127.0.0.1:1", 413},
	} {
		if got := request(tc.body, tc.remote); got != tc.status {
			t.Fatalf("status %d, want %d: %s", got, tc.status, tc.body)
		}
	}
	r.slots = make(chan struct{}, 1)
	r.slots <- struct{}{}
	if got := request(start, "127.0.0.1:1"); got != 503 {
		t.Fatalf("busy status = %d", got)
	}
}

func TestEventReceiverOnMovedPortNotifiesTUI(t *testing.T) {
	st := openTemp(t)
	r := New(st, true)
	r.GRPCAddr, r.HTTPAddr = "127.0.0.1:0", "127.0.0.1:0"
	if err := r.Listen(); err != nil {
		t.Fatal(err)
	}
	if r.HTTPBase() == "" || strings.HasSuffix(r.HTTPBase(), ":0") {
		t.Fatalf("bound endpoint = %q", r.HTTPBase())
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	updates, unsubscribe := st.Subscribe()
	defer unsubscribe()
	request, err := http.NewRequest(http.MethodPost, r.HTTPBase()+"/api/v1/events", strings.NewReader(`{"event_id":"live","type":"span.start","run_id":"live-run","span_id":"live-span","kind":"agent","name":"live","time":"2026-01-01T00:00:00Z"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", response.StatusCode)
	}
	select {
	case <-updates:
	case <-time.After(time.Second):
		t.Fatal("store did not notify TUI")
	}
	runs, err := st.ListRuns(context.Background())
	if err != nil || len(runs) != 1 || runs[0].Source != "events" {
		t.Fatalf("runs: %+v %v", runs, err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("receiver did not stop")
	}
}
