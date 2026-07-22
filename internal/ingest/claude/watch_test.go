package claude

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

func openTemp(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

const lineUser = `{"type":"user","uuid":"u1","timestamp":"2026-07-22T10:00:01Z","cwd":"/home/x/proj","message":{"role":"user","content":"hello"}}` + "\n"

const lineAssistant = `{"type":"assistant","uuid":"a1","parentUuid":"u1","timestamp":"2026-07-22T10:00:03Z","message":{"id":"msg_1","model":"m","role":"assistant","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":1,"output_tokens":2}}}` + "\n"

func waitForSpans(t *testing.T, st *store.Store, runID string, want int) []store.Span {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		spans, err := st.Spans(context.Background(), runID)
		if err != nil {
			t.Fatalf("Spans: %v", err)
		}
		if len(spans) >= want {
			return spans
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("never saw %d spans for %s", want, runID)
	return nil
}

func TestWatchTailsNewAndAppendedFiles(t *testing.T) {
	st := openTemp(t)
	root := t.TempDir()
	proj := filepath.Join(root, "-home-x-proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(proj, "sess-watch.jsonl")
	if err := os.WriteFile(file, []byte(lineUser), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Watch(ctx, st, root, true) }()
	waitForSpans(t, st, "sess-watch", 1) // root span from initial ingest
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(lineAssistant); err != nil {
		t.Fatal(err)
	}
	f.Close()
	spans := waitForSpans(t, st, "sess-watch", 2)
	var llm *store.Span
	for i := range spans {
		if spans[i].Kind == store.KindLLM {
			llm = &spans[i]
		}
	}
	if llm == nil || llm.ID != "msg_1" {
		t.Fatalf("appended llm span missing: %+v", spans)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Watch: %v", err)
	}
}

func TestWatchPicksUpNewProjectDir(t *testing.T) {
	st := openTemp(t)
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Watch(ctx, st, root, true) }()
	time.Sleep(50 * time.Millisecond)
	proj := filepath.Join(root, "-home-x-newproj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	// fsnotify needs a moment to register the new directory watch.
	time.Sleep(200 * time.Millisecond)
	file := filepath.Join(proj, "sess-new.jsonl")
	if err := os.WriteFile(file, []byte(lineUser+lineAssistant), 0o644); err != nil {
		t.Fatal(err)
	}
	waitForSpans(t, st, "sess-new", 2)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Watch: %v", err)
	}
}

func TestWatchSkipsStaleFilesUntilTouched(t *testing.T) {
	st := openTemp(t)
	root := t.TempDir()
	proj := filepath.Join(root, "-home-x-old")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(proj, "sess-old.jsonl")
	if err := os.WriteFile(file, []byte(lineUser), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(file, old, old); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Watch(ctx, st, root, true) }()
	time.Sleep(300 * time.Millisecond)
	if spans, err := st.Spans(context.Background(), "sess-old"); err != nil || len(spans) != 0 {
		t.Fatalf("stale file ingested at startup: %v %v", spans, err)
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(lineAssistant); err != nil {
		t.Fatal(err)
	}
	f.Close()
	spans := waitForSpans(t, st, "sess-old", 2) // full file read on change
	if len(spans) < 2 {
		t.Fatalf("spans = %+v", spans)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Watch: %v", err)
	}
}

func TestWatchRecordsParseErrorFinding(t *testing.T) {
	st := openTemp(t)
	root := t.TempDir()
	proj := filepath.Join(root, "-home-x-bad")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(proj, "sess-bad.jsonl")
	content := "this line is not json\n" + lineUser + lineAssistant
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Watch(ctx, st, root, true) }()
	waitForSpans(t, st, "sess-bad", 2)
	findings, err := st.Findings(context.Background(), "sess-bad")
	if err != nil {
		t.Fatalf("Findings: %v", err)
	}
	if len(findings) != 1 || findings[0].Type != "parse_error" {
		t.Fatalf("findings = %+v", findings)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Watch: %v", err)
	}
}
