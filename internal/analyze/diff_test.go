package analyze

import (
	"context"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

// seedDiffRun writes one run: llm, search_db with the given output, then extras.
func seedDiffRun(t *testing.T, st *store.Store, runID, searchOut string, at time.Time, extraTool string) {
	t.Helper()
	spans := []store.Span{
		{
			ID: runID + "-root", RunID: runID, Kind: store.KindAgent, Name: "agent",
			StartedAt: at, EndedAt: at.Add(10 * time.Second), Status: "ok",
		},
		{
			ID: runID + "-llm", RunID: runID, ParentID: runID + "-root", Kind: store.KindLLM,
			Name: "chat", StartedAt: at, EndedAt: at.Add(2 * time.Second), Status: "ok",
			TokensIn: 100, TokensOut: 50,
		},
		{
			ID: runID + "-search", RunID: runID, ParentID: runID + "-root", Kind: store.KindTool,
			Name: "search_db", StartedAt: at.Add(2 * time.Second),
			EndedAt: at.Add(3 * time.Second), Status: "ok",
			Attrs: store.Attrs{ToolName: "search_db"},
		},
	}
	contents := []store.Content{
		{SpanID: runID + "-llm", Role: "assistant", Seq: 0, Body: "same answer", MediaType: "text/plain"},
		{SpanID: runID + "-search", Role: "output", Seq: 0, Body: searchOut, MediaType: "application/json"},
	}
	if extraTool != "" {
		spans = append(spans, store.Span{
			ID: runID + "-extra", RunID: runID, ParentID: runID + "-root", Kind: store.KindTool,
			Name: extraTool, StartedAt: at.Add(4 * time.Second),
			EndedAt: at.Add(5 * time.Second), Status: "ok",
			Attrs: store.Attrs{ToolName: extraTool},
		})
	}
	if err := st.WriteBatch(context.Background(), store.Batch{
		Source: "test", Spans: spans, Contents: contents,
	}); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
}

// Same tool, different arguments: two separate calls, not one changed step.
func TestDiffRunsSeparatesCallsWithDifferentInput(t *testing.T) {
	st := openTemp(t)
	seed := func(runID, input string, at time.Time) {
		t.Helper()
		spans := []store.Span{{
			ID: runID + "-search", RunID: runID, Kind: store.KindTool,
			Name: "search_db", StartedAt: at, EndedAt: at.Add(time.Second),
			Status: "ok", Attrs: store.Attrs{ToolName: "search_db"},
		}}
		contents := []store.Content{
			{SpanID: runID + "-search", Role: "input", Seq: 0, Body: input, MediaType: "application/json"},
			{SpanID: runID + "-search", Role: "output", Seq: 1, Body: `{"price":42}`, MediaType: "application/json"},
		}
		if err := st.WriteBatch(context.Background(), store.Batch{
			Source: "test", Spans: spans, Contents: contents,
		}); err != nil {
			t.Fatalf("WriteBatch: %v", err)
		}
	}
	seed("ra", `{"sku":"A"}`, t0)
	seed("rb", `{"sku":"B"}`, t0.Add(time.Hour))
	d, err := DiffRuns(context.Background(), st, "ra", "rb")
	if err != nil {
		t.Fatalf("DiffRuns: %v", err)
	}
	if len(d.Steps) != 2 {
		t.Fatalf("steps = %d, want 2 (one only in A, one only in B)", len(d.Steps))
	}
	if d.Steps[0].B != nil || d.Steps[1].A != nil {
		t.Errorf("calls with different arguments were paired: %+v", d.Steps)
	}
}

func TestDiffRunsAlignmentAndDivergence(t *testing.T) {
	st := openTemp(t)
	seedDiffRun(t, st, "ra", `{"price":42}`, t0, "fetch_api")
	seedDiffRun(t, st, "rb", `{"price":"42"}`, t0.Add(time.Hour), "clean")
	d, err := DiffRuns(context.Background(), st, "ra", "rb")
	if err != nil {
		t.Fatalf("DiffRuns: %v", err)
	}
	if len(d.Steps) != 4 {
		t.Fatalf("steps = %d, want 4 (llm, search, fetch_api, clean)", len(d.Steps))
	}
	if d.Steps[0].Diverged {
		t.Errorf("identical llm step diverged: %+v", d.Steps[0])
	}
	if !d.Steps[1].Diverged || d.FirstDivergence != 1 {
		t.Errorf("search step: diverged=%v first=%d", d.Steps[1].Diverged, d.FirstDivergence)
	}
	if d.Steps[1].StepName() != "tool search_db" {
		t.Errorf("step name = %q", d.Steps[1].StepName())
	}
	var onlyA, onlyB int
	for _, s := range d.Steps {
		if s.B == nil {
			onlyA++
		}
		if s.A == nil {
			onlyB++
		}
	}
	if onlyA != 1 || onlyB != 1 {
		t.Errorf("one-sided steps = %d/%d, want 1/1", onlyA, onlyB)
	}
}

func TestDiffDeltas(t *testing.T) {
	st := openTemp(t)
	seedDiffRun(t, st, "ra", `{"price":42}`, t0, "")
	seedDiffRun(t, st, "rb", `{"price":42}`, t0.Add(time.Hour), "")
	// Inflate run b's llm usage and duration.
	b := store.Batch{Source: "test", Spans: []store.Span{{
		ID: "rb-llm", RunID: "rb", ParentID: "rb-root", Kind: store.KindLLM,
		Name: "chat", StartedAt: t0.Add(time.Hour),
		EndedAt: t0.Add(time.Hour + 4*time.Second), Status: "ok",
		TokensIn: 300, TokensOut: 100,
	}}}
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	// Restore the content the span rewrite dropped, so only metrics differ.
	if err := st.WriteBatch(context.Background(), store.Batch{Source: "test", Contents: []store.Content{
		{SpanID: "rb-llm", Role: "assistant", Seq: 0, Body: "same answer", MediaType: "text/plain"},
	}}); err != nil {
		t.Fatalf("WriteBatch contents: %v", err)
	}
	d, err := DiffRuns(context.Background(), st, "ra", "rb")
	if err != nil {
		t.Fatalf("DiffRuns: %v", err)
	}
	llm := d.Steps[0]
	if llm.Diverged {
		t.Errorf("metric-only change marked diverged")
	}
	if llm.DTokens() != 250 {
		t.Errorf("DTokens = %d, want 250", llm.DTokens())
	}
	if llm.DLatency() != 2*time.Second {
		t.Errorf("DLatency = %v, want 2s", llm.DLatency())
	}
	if d.FirstDivergence != -1 {
		t.Errorf("FirstDivergence = %d, want -1", d.FirstDivergence)
	}
}
