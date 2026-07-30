package analyze

import (
	"context"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

// ragBatch builds a retrieval run: a retriever returns docs, one llm answers.
func ragBatch(runID string, docs []string, answer string) store.Batch {
	spans := []store.Span{
		{
			ID: runID + "-root", RunID: runID, Kind: store.KindAgent, Name: "agent",
			StartedAt: t0, EndedAt: t0.Add(10 * time.Second), Status: "ok",
		},
		{
			ID: runID + "-ret", RunID: runID, ParentID: runID + "-root", Kind: store.KindRetrieval,
			Name: "retrieve", StartedAt: t0, EndedAt: t0.Add(2 * time.Second), Status: "ok",
		},
		{
			ID: runID + "-llm", RunID: runID, ParentID: runID + "-root", Kind: store.KindLLM,
			Name: "chat", StartedAt: t0.Add(3 * time.Second), EndedAt: t0.Add(5 * time.Second), Status: "ok",
		},
	}
	var contents []store.Content
	for i, d := range docs {
		contents = append(contents, store.Content{
			SpanID: runID + "-ret", Role: "output", Seq: i, Body: d, MediaType: "text/plain",
		})
	}
	contents = append(contents, store.Content{
		SpanID: runID + "-llm", Role: "assistant", Seq: 0, Body: answer, MediaType: "text/plain",
	})
	return store.Batch{Source: "test", Spans: spans, Contents: contents}
}

func TestUnsupportedFigureFlagged(t *testing.T) {
	st := openTemp(t)
	b := ragBatch("r1",
		[]string{"Refunds are accepted within 30 days. The restocking fee is 250 dollars."},
		"Your refund will be processed and the restocking fee is 495 dollars.")
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	sweep(t, st)
	fs := runFindings(t, st, "r1")
	if len(fs) != 1 || fs[0].Type != "unsupported_claim" || fs[0].SpanID != "r1-llm" {
		t.Fatalf("findings = %+v", fs)
	}
}

func TestGroundedFigureNotFlagged(t *testing.T) {
	st := openTemp(t)
	// 4,200 in the doc and 4200 in the answer are the same figure once thousands
	// separators are dropped.
	b := ragBatch("r1", []string{"The annual price is 4,200 USD."}, "It costs 4200 USD per year.")
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	sweep(t, st)
	if fs := runFindings(t, st, "r1"); len(fs) != 0 {
		t.Fatalf("grounded figure flagged: %+v", fs)
	}
}

// Small counts are arithmetic-prone and not distinctive, so a differing one-
// or two-digit number is not treated as an unsupported figure.
func TestSmallNumbersNotFlagged(t *testing.T) {
	st := openTemp(t)
	b := ragBatch("r1", []string{"Standard orders ship in 5 days."}, "Your order ships in 7 days.")
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	sweep(t, st)
	if fs := runFindings(t, st, "r1"); len(fs) != 0 {
		t.Fatalf("small number flagged: %+v", fs)
	}
}

// A run with no retriever is not a RAG run, so the check stays silent even when
// the answer carries a distinctive number.
func TestNoRetrievalNoFaithfulnessCheck(t *testing.T) {
	st := openTemp(t)
	b := store.Batch{Source: "test", Spans: []store.Span{
		{ID: "r1-root", RunID: "r1", Kind: store.KindAgent, Name: "agent", StartedAt: t0, EndedAt: t0.Add(5 * time.Second), Status: "ok"},
		{ID: "r1-llm", RunID: "r1", ParentID: "r1-root", Kind: store.KindLLM, Name: "chat", StartedAt: t0, EndedAt: t0.Add(2 * time.Second), Status: "ok"},
	}, Contents: []store.Content{
		{SpanID: "r1-llm", Role: "assistant", Seq: 0, Body: "The total is 875 dollars.", MediaType: "text/plain"},
	}}
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	sweep(t, st)
	if fs := runFindings(t, st, "r1"); len(fs) != 0 {
		t.Fatalf("non-rag run flagged: %+v", fs)
	}
}
