package store

import (
	"context"
	"testing"
	"time"
)

func TestSearchRunsFiltersBeforePagination(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	for i, env := range []string{"production", "staging", "production"} {
		id := string(rune('a' + i))
		sp := Span{ID: "span-" + id, RunID: "run-" + id, Kind: KindAgent,
			Name: "agent", StartedAt: t0.Add(time.Duration(i) * time.Minute),
			EndedAt: t0.Add(time.Duration(i)*time.Minute + time.Second), Status: "ok"}
		b := Batch{Source: "test", Spans: []Span{sp}, RunContexts: map[string]RunContext{
			sp.RunID: {Environment: env, Release: "v1"},
		}}
		if err := s.WriteBatch(ctx, b); err != nil {
			t.Fatal(err)
		}
		if err := s.SetRunLabel(ctx, sp.RunID, "test", "case "+id); err != nil {
			t.Fatal(err)
		}
	}
	filter := RunSearch{Project: "default", Environment: "production", Query: "case", Limit: 1, Offset: 1}
	runs, err := s.SearchRuns(ctx, filter)
	if err != nil || len(runs) != 1 || runs[0].ID != "run-a" {
		t.Fatalf("filter after pagination or wrong order: %+v, %v", runs, err)
	}
	filter.Query = "%"
	runs, err = s.SearchRuns(ctx, filter)
	if err != nil || len(runs) != 0 {
		t.Fatalf("LIKE wildcard was not escaped: %+v, %v", runs, err)
	}
	filter.Query = ""
	filter.From = t0.Add(time.Minute)
	runs, err = s.SearchRuns(ctx, filter)
	if err != nil || len(runs) != 0 {
		t.Fatalf("time filter missed cutoff: %+v, %v", runs, err)
	}
	runs, err = s.SearchRuns(ctx, RunSearch{Project: "default", From: t0, To: t0.Add(time.Minute), Limit: 100})
	if err != nil || len(runs) != 1 || runs[0].ID != "run-a" {
		t.Fatalf("adjacent time windows overlap: %+v, %v", runs, err)
	}
}
