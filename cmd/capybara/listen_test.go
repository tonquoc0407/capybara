package main

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

func TestListenRejectsArguments(t *testing.T) {
	if err := listenCmd(context.Background(), filepath.Join(t.TempDir(), "test.db"), "", true, []string{"extra"}, io.Discard); err == nil {
		t.Fatal("extra argument accepted")
	}
}

func TestListenHeadlessReceivesEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, writer := io.Pipe()
	defer reader.Close()
	lines := make(chan string, 4)
	go func() {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"-db", dbPath, "-otlp", "127.0.0.1:0", "listen"}, writer)
		writer.Close()
	}()
	var first string
	select {
	case first = <-lines:
	case <-time.After(2 * time.Second):
		t.Fatal("listen printed no endpoint")
	}
	select {
	case <-lines:
	case <-time.After(2 * time.Second):
		t.Fatal("listen printed no OTLP endpoint")
	}
	endpoint := strings.TrimSpace(strings.TrimPrefix(first, "events "))
	if !strings.HasPrefix(endpoint, "http://127.0.0.1:") || !strings.HasSuffix(endpoint, "/api/v1/events") {
		t.Fatalf("endpoint: %q", endpoint)
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(`{"event_id":"headless","type":"span.complete","run_id":"headless-run","span_id":"headless-span","kind":"agent","name":"job","time":"2026-01-01T00:00:01Z","start_time":"2026-01-01T00:00:00Z","status":"ok"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	for _, payload := range []string{
		`{"event_id":"tool-error","type":"span.complete","run_id":"headless-run","span_id":"headless-tool","kind":"tool","name":"lookup","tool_name":"lookup","time":"2026-01-01T00:00:03Z","start_time":"2026-01-01T00:00:02Z","status":"ok","contents":[{"role":"output","body":"{\"error\":\"unavailable\"}","media_type":"application/json"}]}`,
		`{"event_id":"llm-priced","type":"span.complete","run_id":"headless-run","span_id":"headless-llm","kind":"llm","name":"chat","model":"gpt-4o-mini","tokens_in":1000,"tokens_out":100,"time":"2026-01-01T00:00:05Z","start_time":"2026-01-01T00:00:04Z","status":"ok"}`,
	} {
		req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("status = %d for %s", resp.StatusCode, payload)
		}
	}
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ready := false
	for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
		run, runErr := st.GetRun(context.Background(), "headless-run")
		findings, findingErr := st.Findings(context.Background(), "headless-run")
		if runErr == nil && findingErr == nil && run.CostUSD != nil && *run.CostUSD > 0 {
			for _, f := range findings {
				if f.Type == "tool_error" && f.SpanID == "headless-tool" {
					ready = true
				}
			}
		}
		if ready {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatal("listener stored events but analyzer did not produce the tool finding and cost")
	}
	client.CloseIdleConnections()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("listen did not stop")
	}
	runs, err := st.ListRuns(context.Background())
	if err != nil || len(runs) != 1 || runs[0].ID != "headless-run" {
		t.Fatalf("stored runs: %+v %v", runs, err)
	}
}
