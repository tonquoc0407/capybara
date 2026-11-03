package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSpanEventsMergeRetryAndPrivacy(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	model := "known-model"
	input, output := int64(12), int64(8)
	start := SpanEvent{EventID: "e1", Type: "span.start", RunID: "r", SpanID: "s", Kind: "llm", Name: "chat", Time: t0, Model: &model, Attrs: map[string]any{"secret": "do not store"}, Contents: []EventContent{{Role: "user", Body: "secret"}}}
	if created, err := s.ApplySpanEvent(ctx, start, false); err != nil || !created {
		t.Fatalf("start: %v %v", created, err)
	}
	if created, err := s.ApplySpanEvent(ctx, start, false); err != nil || created {
		t.Fatalf("retry: %v %v", created, err)
	}
	changed := start
	changed.Name = "different"
	if _, err := s.ApplySpanEvent(ctx, changed, false); !errors.Is(err, ErrEventConflict) {
		t.Fatalf("changed retry: %v", err)
	}
	child := SpanEvent{EventID: "child", Type: "span.complete", RunID: "r", SpanID: "c", ParentID: "s", Kind: "tool", Name: "lookup", Time: t0.Add(time.Second), Status: "ok"}
	if _, err := s.ApplySpanEvent(ctx, child, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplySpanEvent(ctx, SpanEvent{EventID: "e2", Type: "span.update", RunID: "r", SpanID: "s", Time: t0.Add(time.Second), TokensIn: &input}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplySpanEvent(ctx, SpanEvent{EventID: "e3", Type: "span.end", RunID: "r", SpanID: "s", Time: t0.Add(2 * time.Second), TokensOut: &output, Status: "ok"}, false); err != nil {
		t.Fatal(err)
	}
	spans, err := s.Spans(ctx, "r")
	if err != nil || len(spans) != 2 {
		t.Fatalf("spans: %+v %v", spans, err)
	}
	var root Span
	for _, sp := range spans {
		if sp.ID == "s" {
			root = sp
		}
	}
	if root.Attrs.Model != model || root.TokensIn != input || root.TokensOut != output || root.Status != "ok" || root.EndedAt.IsZero() {
		t.Fatalf("merged span: %+v", root)
	}
	contents, err := s.Contents(ctx, "s")
	if err != nil || len(contents) != 0 {
		t.Fatalf("privacy contents: %+v %v", contents, err)
	}
	if _, exists := root.Attrs.Raw["secret"]; exists {
		t.Fatalf("privacy attrs: %+v", root.Attrs)
	}
	runs, err := s.ListRuns(ctx)
	if err != nil || len(runs) != 1 || runs[0].TokensIn != input || runs[0].Status != "ok" {
		t.Fatalf("run: %+v %v", runs, err)
	}
}

func TestSpanEventEndBeforeStart(t *testing.T) {
	s := openTemp(t)
	_, err := s.ApplySpanEvent(context.Background(), SpanEvent{EventID: "late", Type: "span.end", RunID: "r", SpanID: "s", Time: t0, Status: "ok"}, true)
	if !errors.Is(err, ErrSpanNotStarted) {
		t.Fatalf("got %v", err)
	}
}

func TestSpanEventContentIsAppendedOnce(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	e := SpanEvent{EventID: "a", Type: "span.start", RunID: "r", SpanID: "s", Kind: "agent", Name: "run", Time: t0,
		Contents: []EventContent{{Role: "user", Body: "first"}}}
	if _, err := s.ApplySpanEvent(ctx, e, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplySpanEvent(ctx, e, true); err != nil {
		t.Fatal(err)
	}
	e.EventID, e.Type, e.Contents = "b", "span.update", []EventContent{{Role: "assistant", Body: "second"}}
	e.Kind, e.Name = "", ""
	if _, err := s.ApplySpanEvent(ctx, e, true); err != nil {
		t.Fatal(err)
	}
	contents, err := s.Contents(ctx, "s")
	if err != nil || len(contents) != 2 || contents[0].Body != "first" || contents[1].Body != "second" {
		t.Fatalf("contents: %+v %v", contents, err)
	}
}

func TestEventRunCountIncludesExistingOTLPRun(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.WriteBatch(ctx, Batch{Source: "otlp", Spans: []Span{{ID: "otlp-span", RunID: "shared", Kind: KindAgent, Name: "root", StartedAt: t0, Status: "running"}}}); err != nil {
		t.Fatal(err)
	}
	e := SpanEvent{EventID: "event", Type: "span.start", RunID: "shared", SpanID: "event-span", Kind: "tool", Name: "call", Time: t0}
	if _, err := s.ApplySpanEvent(ctx, e, true); err != nil {
		t.Fatal(err)
	}
	count, err := s.EventRunCount(ctx)
	if err != nil || count != 1 {
		t.Fatalf("event run count: %d %v", count, err)
	}
}

func TestEventReportedCostSurvivesLateUsage(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	cost := 0.0123
	input := int64(200)
	e := SpanEvent{EventID: "start", Type: "span.start", RunID: "r", SpanID: "s", Kind: "llm", Name: "chat", Time: t0}
	if _, err := s.ApplySpanEvent(ctx, e, true); err != nil {
		t.Fatal(err)
	}
	e = SpanEvent{EventID: "end", Type: "span.end", RunID: "r", SpanID: "s", Time: t0.Add(time.Second), Status: "ok", CostUSD: &cost}
	if _, err := s.ApplySpanEvent(ctx, e, true); err != nil {
		t.Fatal(err)
	}
	e = SpanEvent{EventID: "usage", Type: "span.update", RunID: "r", SpanID: "s", Time: t0.Add(2 * time.Second), TokensIn: &input}
	if _, err := s.ApplySpanEvent(ctx, e, true); err != nil {
		t.Fatal(err)
	}
	spans, err := s.Spans(ctx, "r")
	if err != nil || len(spans) != 1 || spans[0].CostUSD == nil || *spans[0].CostUSD != cost {
		t.Fatalf("reported cost lost: %+v %v", spans, err)
	}
	provenance, err := s.CostProvenanceForRun(ctx, "r")
	if err != nil || len(provenance) != 1 || provenance[0].Kind != "provider" || provenance[0].PriceSource != "application event" {
		t.Fatalf("provenance: %+v %v", provenance, err)
	}
	if err := s.SetAutomaticSpanEstimates(ctx, map[string]SpanEstimate{"s": {CostUSD: 9, Revision: spans[0].Revision, Provenance: CostProvenance{Kind: "estimate", PriceSource: "capybara embedded pricing"}}}); err != nil {
		t.Fatal(err)
	}
	spans, err = s.Spans(ctx, "r")
	if err != nil || *spans[0].CostUSD != cost {
		t.Fatalf("estimate replaced reported cost: %+v %v", spans, err)
	}
	runs, err := s.ListRuns(ctx)
	if err != nil || len(runs) != 1 || runs[0].CostUSD == nil || *runs[0].CostUSD != cost {
		t.Fatalf("run cost: %+v %v", runs, err)
	}
}
