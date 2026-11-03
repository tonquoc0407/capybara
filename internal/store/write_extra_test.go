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

func TestRepeatedSpanKeepsRecordedEvidenceAndCost(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	b := testBatch()
	if err := s.WriteBatch(ctx, b); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSpanCosts(ctx, map[string]float64{"llm1": 0.02}); err != nil {
		t.Fatal(err)
	}
	// A retry can contain only span metadata. It must not erase already
	// recorded bodies or the cost assigned after the first import.
	b.Spans = b.Spans[1:2]
	b.Contents = nil
	if err := s.WriteBatch(ctx, b); err != nil {
		t.Fatal(err)
	}
	content, err := s.Contents(ctx, "llm1")
	if err != nil || len(content) != 2 {
		t.Fatalf("content after retry = %+v, %v", content, err)
	}
	spans, err := s.Spans(ctx, "r1")
	if err != nil || spans[1].CostUSD == nil || *spans[1].CostUSD != 0.02 {
		t.Fatalf("cost after retry = %+v, %v", spans, err)
	}
	provenance, err := s.CostProvenanceForRun(ctx, "r1")
	if err != nil || len(provenance) != 1 || provenance[0].SpanID != "llm1" {
		t.Fatalf("cost provenance after retry = %+v, %v", provenance, err)
	}
}

func TestReimportClearsStaleEstimateAndKeepsProviderCost(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	b := testBatch()
	if err := s.WriteBatch(ctx, b); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSpanCosts(ctx, map[string]float64{"llm1": 0.02}); err != nil {
		t.Fatal(err)
	}
	b.Spans = b.Spans[1:2]
	b.Contents = nil
	b.Spans[0].Attrs.Model = "unknown-model"
	if err := s.WriteBatch(ctx, b); err != nil {
		t.Fatal(err)
	}
	spans, err := s.Spans(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	for _, sp := range spans {
		if sp.ID == "llm1" && sp.CostUSD != nil {
			t.Fatalf("stale estimate = %v", *sp.CostUSD)
		}
	}
	provenance, err := s.CostProvenanceForRun(ctx, "r1")
	if err != nil || len(provenance) != 0 {
		t.Fatalf("stale provenance: %+v %v", provenance, err)
	}
	if err := s.SetSpanEstimates(ctx, map[string]SpanEstimate{"llm1": {CostUSD: 0.03, Provenance: CostProvenance{Kind: "provider", PriceSource: "receipt"}}}); err != nil {
		t.Fatal(err)
	}
	b.Spans[0].TokensIn++
	if err := s.WriteBatch(ctx, b); err != nil {
		t.Fatal(err)
	}
	spans, err = s.Spans(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	for _, sp := range spans {
		if sp.ID == "llm1" && (sp.CostUSD == nil || *sp.CostUSD != 0.03) {
			t.Fatalf("provider cost changed: %+v", sp)
		}
	}
}

func TestExplicitImportCostReplacesEstimateProvenance(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	b := testBatch()
	if err := s.WriteBatch(ctx, b); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSpanCosts(ctx, map[string]float64{"llm1": 0.02}); err != nil {
		t.Fatal(err)
	}
	b.Spans = b.Spans[1:2]
	b.Contents = nil
	cost := 0.04
	b.Spans[0].CostUSD = &cost
	if err := s.WriteBatch(ctx, b); err != nil {
		t.Fatal(err)
	}
	spans, err := s.Spans(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	for _, sp := range spans {
		if sp.ID == "llm1" && (sp.CostUSD == nil || *sp.CostUSD != cost) {
			t.Fatalf("explicit cost: %+v", sp)
		}
	}
	provenance, err := s.CostProvenanceForRun(ctx, "r1")
	if err != nil || len(provenance) != 0 {
		t.Fatalf("old provenance attached to explicit cost: %+v %v", provenance, err)
	}
	usage, err := s.UsageByProject(ctx)
	if err != nil || len(usage) != 1 || usage[0].EstimatedUSD != 0 || usage[0].UnattributedUSD != cost {
		t.Fatalf("usage classification: %+v %v", usage, err)
	}
}

func TestWriteBatchRejectsNegativeImportedCost(t *testing.T) {
	s := openTemp(t)
	b := testBatch()
	cost := -0.01
	b.Spans[1].CostUSD = &cost
	if err := s.WriteBatch(context.Background(), b); err == nil {
		t.Fatal("negative cost accepted")
	}
	runs, err := s.ListRuns(context.Background())
	if err != nil || len(runs) != 0 {
		t.Fatalf("partial write after invalid cost: %+v %v", runs, err)
	}
}

func TestSpanIDCannotMoveBetweenRuns(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.WriteBatch(ctx, testBatch()); err != nil {
		t.Fatal(err)
	}
	b := Batch{Source: "test", Spans: []Span{{ID: "llm1", RunID: "other", Kind: KindLLM, Name: "collision"}}}
	if err := s.WriteBatch(ctx, b); err == nil {
		t.Fatal("a span ID was moved to another trace")
	}
	spans, err := s.Spans(ctx, "r1")
	if err != nil || len(spans) != 3 {
		t.Fatalf("original run changed: %d spans, %v", len(spans), err)
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
