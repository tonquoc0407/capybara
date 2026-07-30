package analyze

import (
	"context"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

func truncBatch(runID string, raw map[string]any, laterTurn bool) store.Batch {
	spans := []store.Span{
		{
			ID: runID + "-root", RunID: runID, Kind: store.KindAgent, Name: "agent",
			StartedAt: t0, EndedAt: t0.Add(10 * time.Second), Status: "ok",
		},
		{
			ID: runID + "-llm", RunID: runID, ParentID: runID + "-root", Kind: store.KindLLM,
			Name: "chat", StartedAt: t0, EndedAt: t0.Add(2 * time.Second), Status: "ok",
			Attrs: store.Attrs{Raw: raw},
		},
	}
	if laterTurn {
		spans = append(spans, store.Span{
			ID: runID + "-llm2", RunID: runID, ParentID: runID + "-root", Kind: store.KindLLM,
			Name: "chat", StartedAt: t0.Add(3 * time.Second), EndedAt: t0.Add(5 * time.Second), Status: "ok",
		})
	}
	contents := []store.Content{
		{SpanID: runID + "-llm", Role: "assistant", Seq: 0, Body: "The answer is", MediaType: "text/plain"},
	}
	return store.Batch{Source: "test", Spans: spans, Contents: contents}
}

func TestTruncatedTerminalFlagged(t *testing.T) {
	st := openTemp(t)
	b := truncBatch("r1", map[string]any{"gen_ai.response.finish_reasons": []any{"length"}}, false)
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	sweep(t, st)
	fs := runFindings(t, st, "r1")
	if len(fs) != 1 || fs[0].Type != "truncated" || fs[0].SpanID != "r1-llm" {
		t.Fatalf("findings = %+v", fs)
	}
}

// A finish reason other than the token limit is normal completion.
func TestNotTruncatedOnStop(t *testing.T) {
	st := openTemp(t)
	b := truncBatch("r1", map[string]any{"gen_ai.response.finish_reasons": []any{"stop"}}, false)
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	sweep(t, st)
	if fs := runFindings(t, st, "r1"); len(fs) != 0 {
		t.Fatalf("stop reason flagged: %+v", fs)
	}
}

// A truncated turn the model followed with another turn was not its last word.
func TestNotTruncatedWhenNotTerminal(t *testing.T) {
	st := openTemp(t)
	b := truncBatch("r1", map[string]any{"ai.response.finishReason": "length"}, true)
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	sweep(t, st)
	if fs := runFindings(t, st, "r1"); len(fs) != 0 {
		t.Fatalf("non-terminal truncation flagged: %+v", fs)
	}
}

// The reason is read whatever the convention calls the field.
func TestTruncatedReadsAnyConventionKey(t *testing.T) {
	st := openTemp(t)
	b := truncBatch("r1", map[string]any{"ai.response.finishReason": "length"}, false)
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	sweep(t, st)
	if fs := runFindings(t, st, "r1"); len(fs) != 1 || fs[0].Type != "truncated" {
		t.Fatalf("findings = %+v", fs)
	}
}
