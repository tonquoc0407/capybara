package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestOperationsMigrationAndLateLogs(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	log := TraceLog{ID: "log-1", RunID: "trace-1", SpanID: "span-1", ObservedAt: at,
		Severity: "ERROR", Body: "failed", AttrsJSON: `{}`}
	for range 2 {
		if err := st.PutTraceLogs(ctx, "otlp", []TraceLog{log}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.WriteBatch(ctx, Batch{Source: "otlp", RunContexts: map[string]RunContext{
		"trace-1": {Project: "payments", Environment: "prod", Release: "v2", SessionID: "s1"},
	}, Spans: []Span{{ID: "span-1", RunID: "trace-1", Kind: KindTool,
		Name: "charge", StartedAt: at, EndedAt: at.Add(time.Second), Status: "error"}}}); err != nil {
		t.Fatal(err)
	}
	logs, err := st.TraceLogs(ctx, "trace-1")
	if err != nil || len(logs) != 1 || logs[0].Body != "failed" {
		t.Fatalf("logs: %+v, %v", logs, err)
	}
	headers, err := st.TraceLogHeaders(ctx, "trace-1")
	if err != nil || len(headers) != 1 || headers[0].Body != "" || headers[0].Severity != "ERROR" {
		t.Fatalf("log headers: %+v, %v", headers, err)
	}
	runs, err := st.ListRunsPage(ctx, "payments", 1, 0)
	if err != nil || len(runs) != 1 || runs[0].Environment != "prod" || runs[0].Release != "v2" {
		t.Fatalf("runs: %+v, %v", runs, err)
	}
	if _, err := st.ListRunsPage(ctx, "", 101, 0); err == nil {
		t.Fatal("accepted unbounded page")
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	logs, err = st.TraceLogs(ctx, "trace-1")
	if err != nil || len(logs) != 1 {
		t.Fatalf("logs after reopen: %+v, %v", logs, err)
	}
}
