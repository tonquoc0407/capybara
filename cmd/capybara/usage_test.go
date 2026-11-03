package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

func TestUsageWindowReportsReleaseAndRejectsBadBounds(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "usage.db")
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	if err := st.WriteBatch(ctx, store.Batch{
		Source: "test", Spans: []store.Span{{
			ID: "span", RunID: "run", Kind: store.KindAgent,
			Name: "agent", Status: "error", StartedAt: at, EndedAt: at.Add(time.Second),
		}},
		RunContexts: map[string]store.RunContext{"run": {Project: "p", Release: "v2"}},
	}); err != nil {
		t.Fatal(err)
	}
	st.Close()
	var out bytes.Buffer
	args := []string{"-db", db, "usage", "window", "-project", "p", "-release", "v2", "-from", "2026-10-18T00:00:00Z", "-to", "2026-10-19T00:00:00Z"}
	if err := run(ctx, args, &out); err != nil {
		t.Fatal(err)
	}
	var report store.OperationsReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Total.Runs != 1 || report.Total.Errors != 1 || report.Total.P95LatencyMS == nil || *report.Total.P95LatencyMS != 1000 || report.Release != "v2" {
		t.Fatalf("report: %+v", report)
	}
	for _, tail := range [][]string{{"-from", "yesterday"}, {"-from", "2026-10-19T00:00:00Z", "-to", "2026-10-18T00:00:00Z"}} {
		if err := run(ctx, append([]string{"-db", db, "usage", "window"}, tail...), &out); err == nil {
			t.Fatalf("invalid bounds accepted: %v", tail)
		}
	}
}
