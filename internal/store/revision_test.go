package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestAnalysisAcknowledgesOnlyObservedRevisionsAcrossBatches(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	batch := Batch{Source: "test"}
	for i := range 600 {
		batch.Spans = append(batch.Spans, Span{
			ID: fmt.Sprintf("span-%d", i), RunID: "r",
			Kind: KindLLM, Name: "chat", Status: "ok", StartedAt: t0, EndedAt: t0.Add(time.Second),
		})
	}
	if err := s.WriteBatch(ctx, batch); err != nil {
		t.Fatal(err)
	}
	observed, err := s.UnanalyzedSpans(ctx)
	if err != nil || len(observed) != 600 {
		t.Fatalf("pending: %d, %v", len(observed), err)
	}
	late := observed[300]
	late.TokensIn = 321
	if err := s.WriteBatch(ctx, Batch{Source: "late usage", Spans: []Span{late}}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkAnalyzed(ctx, observed); err != nil {
		t.Fatal(err)
	}
	pending, err := s.UnanalyzedSpans(ctx)
	if err != nil || len(pending) != 1 || pending[0].ID != late.ID || pending[0].TokensIn != 321 || pending[0].Revision <= late.Revision {
		t.Fatalf("late revision lost: %+v, %v", pending, err)
	}
	if err := s.MarkAnalyzed(ctx, pending); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteBatch(ctx, Batch{Source: "late body", Contents: []Content{{SpanID: late.ID, Role: "output", Body: "late answer"}}}); err != nil {
		t.Fatal(err)
	}
	pending, err = s.UnanalyzedSpans(ctx)
	if err != nil || len(pending) != 1 || pending[0].ID != late.ID {
		t.Fatalf("late content did not requeue analysis: %+v, %v", pending, err)
	}
	if err := s.WriteBatch(ctx, Batch{Source: "foreign", HubProject: "other", Contents: []Content{{SpanID: late.ID, Role: "output", Body: "foreign answer"}}}); err == nil {
		t.Fatal("cross-project content-only update accepted")
	}
	contents, err := s.Contents(ctx, late.ID)
	if err != nil || len(contents) != 1 || contents[0].Body != "late answer" {
		t.Fatalf("content evidence changed: %+v, %v", contents, err)
	}
}

func TestAutomaticEstimateSkipsChangedSpan(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	sp := Span{
		ID: "llm", RunID: "r", Kind: KindLLM, Name: "chat", Status: "ok",
		StartedAt: t0, EndedAt: t0.Add(time.Second), TokensIn: 20,
		Attrs: Attrs{Model: "known"},
	}
	if err := s.WriteBatch(ctx, Batch{Source: "test", Spans: []Span{sp}}); err != nil {
		t.Fatal(err)
	}
	observed, err := s.Spans(ctx, "r")
	if err != nil {
		t.Fatal(err)
	}
	sp.Attrs.Model = "unknown"
	if err := s.WriteBatch(ctx, Batch{Source: "late", Spans: []Span{sp}}); err != nil {
		t.Fatal(err)
	}
	err = s.SetAutomaticSpanEstimates(ctx, map[string]SpanEstimate{"llm": {
		CostUSD: 0.05, Revision: observed[0].Revision,
		Provenance: CostProvenance{Kind: "estimate", PriceSource: "capybara embedded pricing"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	current, err := s.Spans(ctx, "r")
	if err != nil || len(current) != 1 || current[0].CostUSD != nil {
		t.Fatalf("stale estimate stored: %+v %v", current, err)
	}
}
