package store

import (
	"context"
	"testing"
)

func TestSetSpanCostsPricesSpansAndRefreshesRunTotal(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.WriteBatch(ctx, testBatch()); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	if err := s.SetSpanCosts(ctx, map[string]float64{"llm1": 0.02, "tool1": 0.01}); err != nil {
		t.Fatalf("SetSpanCosts: %v", err)
	}
	spans, err := s.Spans(ctx, "r1")
	if err != nil {
		t.Fatalf("Spans: %v", err)
	}
	for _, sp := range spans {
		if sp.ID == "llm1" && (sp.CostUSD == nil || *sp.CostUSD != 0.02) {
			t.Errorf("llm1 cost = %v, want 0.02", sp.CostUSD)
		}
	}
	runs, err := s.ListRuns(ctx)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if runs[0].CostUSD == nil || *runs[0].CostUSD != 0.03 {
		t.Errorf("run cost = %v, want 0.03", runs[0].CostUSD)
	}
}

func TestSetSpanCostsEmptyIsANoop(t *testing.T) {
	s := openTemp(t)
	if err := s.SetSpanCosts(context.Background(), nil); err != nil {
		t.Fatalf("SetSpanCosts(nil): %v", err)
	}
}

func TestSetSpanCostsUnknownSpanErrors(t *testing.T) {
	s := openTemp(t)
	if err := s.SetSpanCosts(context.Background(), map[string]float64{"nope": 1}); err == nil {
		t.Error("expected an error pricing an unknown span")
	}
}

func TestSetRunParentCreatesRowBeforeSpansArrive(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.WriteBatch(ctx, testBatch()); err != nil { // the parent run must exist for the FK
		t.Fatalf("WriteBatch: %v", err)
	}
	if err := s.SetRunParent(ctx, "child", "replay", "r1"); err != nil {
		t.Fatalf("SetRunParent: %v", err)
	}
	runs, err := s.ListRuns(ctx)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	var found bool
	for _, r := range runs {
		if r.ID == "child" {
			found = true
			if r.ParentRunID != "r1" {
				t.Errorf("parent = %q, want r1", r.ParentRunID)
			}
		}
	}
	if !found {
		t.Fatal("child run was not created")
	}
}

func TestSetRunParentUpdatesExistingRow(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.WriteBatch(ctx, testBatch()); err != nil {
		t.Fatalf("WriteBatch r1: %v", err)
	}
	if err := s.WriteBatch(ctx, otherRunBatch("r2")); err != nil {
		t.Fatalf("WriteBatch r2: %v", err)
	}
	if err := s.SetRunParent(ctx, "r1", "otlp", "r2"); err != nil {
		t.Fatalf("SetRunParent: %v", err)
	}
	runs, err := s.ListRuns(ctx)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	for _, r := range runs {
		if r.ID == "r1" && r.ParentRunID != "r2" {
			t.Errorf("parent = %q, want r2", r.ParentRunID)
		}
	}
}
