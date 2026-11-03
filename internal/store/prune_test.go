package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestStorePrune(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "capybara.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := time.Now().UTC()
	for i := 1; i <= 5; i++ {
		started := now.Add(-time.Duration(10-i) * time.Hour)
		status := "ok"
		if i%2 == 0 {
			status = "error"
		}
		b := Batch{
			Source: "test",
			Spans: []Span{
				{
					ID:        fmtRunSpan("run", i, "s1"),
					RunID:     fmtRunSpan("run", i, ""),
					Kind:      KindLLM,
					Name:      "chat",
					StartedAt: started,
					EndedAt:   started.Add(time.Second),
					Status:    status,
				},
			},
			Contents: []Content{
				{
					SpanID:    fmtRunSpan("run", i, "s1"),
					Role:      "assistant",
					Seq:       0,
					Body:      "hello",
					MediaType: "text/plain",
				},
			},
		}
		if err := st.WriteBatch(ctx, b); err != nil {
			t.Fatal(err)
		}
	}

	runs, err := st.ListRuns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 5 {
		t.Fatalf("want 5 runs, got %d", len(runs))
	}

	// Test DryRun
	deleted, err := st.Prune(ctx, PruneOptions{Keep: 2, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 3 {
		t.Fatalf("want dry run to report 3 deleted, got %d", deleted)
	}

	// Prune Keep: 2 (delete 3 oldest)
	deleted, err = st.Prune(ctx, PruneOptions{Keep: 2})
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 3 {
		t.Fatalf("want 3 deleted, got %d", deleted)
	}

	runs, err = st.ListRuns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Fatalf("want 2 runs remaining, got %d", len(runs))
	}

	// Vacuum
	if err := st.Vacuum(ctx); err != nil {
		t.Fatal(err)
	}
}

func fmtRunSpan(prefix string, i int, suffix string) string {
	if suffix == "" {
		return prefix + "-" + string(rune('0'+i))
	}
	return prefix + "-" + string(rune('0'+i)) + "-" + suffix
}

func TestPruneKeepsQualityEvidenceAndDeletesLinkedLogs(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "quality-prune.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, id := range []string{"pinned", "removable"} {
		sp := Span{ID: id + "-span", RunID: id, Kind: KindAgent, Name: "answer",
			StartedAt: time.Now(), EndedAt: time.Now(), Status: "ok"}
		if err := st.WriteBatch(ctx, Batch{Source: "test", Spans: []Span{sp}, Contents: []Content{
			{SpanID: sp.ID, Role: "user", Seq: 0, Body: "q", MediaType: "text/plain"},
			{SpanID: sp.ID, Role: "assistant", Seq: 1, Body: "a", MediaType: "text/plain"},
		}}); err != nil {
			t.Fatal(err)
		}
		if err := st.PutTraceLogs(ctx, "test", []TraceLog{{ID: id + "-log", RunID: id,
			SpanID: sp.ID, Body: "log", AttrsJSON: `{}`}}); err != nil {
			t.Fatal(err)
		}
	}
	d, err := st.CreateDataset(ctx, "default", "cases")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddDatasetItem(ctx, d.ID, "pinned"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddScore(ctx, "removable", "", "quality", 0.5, "human", ""); err != nil {
		t.Fatal(err)
	}
	deleted, err := st.Prune(ctx, PruneOptions{CleanOK: true})
	if err != nil || deleted != 1 {
		t.Fatalf("prune: %d, %v", deleted, err)
	}
	if !st.RunBelongsToProject(ctx, "pinned", "default") || st.RunBelongsToProject(ctx, "removable", "default") {
		t.Fatal("wrong runs retained")
	}
	logs, err := st.TraceLogs(ctx, "removable")
	if err != nil || len(logs) != 0 {
		t.Fatalf("logs retained: %+v, %v", logs, err)
	}
}
