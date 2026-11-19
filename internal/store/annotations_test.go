package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestIncidentAnnotationsValidateAndOrder(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "annotations.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := time.Now().UTC()
	if err := st.WriteBatch(ctx, Batch{
		Source: "test", RunContexts: map[string]RunContext{"run-1": {Project: "checkout"}},
		Spans: []Span{{ID: "span-1", RunID: "run-1", Kind: KindAgent, Name: "agent", Status: "ok", StartedAt: at, EndedAt: at}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddIncidentAnnotation(ctx, "run-1", "bad", "note", "me"); err == nil {
		t.Fatal("accepted invalid status")
	}
	first, err := st.AddIncidentAnnotation(ctx, "run-1", "investigating", "checked tool timeout", "")
	if err != nil || first.Author != "local" {
		t.Fatalf("first annotation: %+v, %v", first, err)
	}
	second, err := st.AddIncidentAnnotation(ctx, "run-1", "resolved", "rolled back release", "me")
	if err != nil {
		t.Fatal(err)
	}
	items, err := st.IncidentAnnotations(ctx, "run-1")
	if err != nil || len(items) != 2 || items[0].ID != first.ID || items[1].ID != second.ID {
		t.Fatalf("annotations: %+v, %v", items, err)
	}
}
