package analyze

import (
	"context"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

func turnRun(runID string, answers []string) store.Batch {
	spans := []store.Span{{
		ID: runID + "-root", RunID: runID, Kind: store.KindAgent, Name: "agent",
		StartedAt: t0, EndedAt: t0.Add(time.Duration(len(answers)+1) * time.Second), Status: "ok",
	}}
	var contents []store.Content
	for i, a := range answers {
		id := runID + "-llm" + string(rune('a'+i))
		spans = append(spans, store.Span{
			ID: id, RunID: runID, ParentID: runID + "-root", Kind: store.KindLLM, Name: "chat",
			StartedAt: t0.Add(time.Duration(i) * time.Second),
			EndedAt:   t0.Add(time.Duration(i+1) * time.Second), Status: "ok",
		})
		contents = append(contents, store.Content{
			SpanID: id, Role: "assistant", Seq: 0, Body: a, MediaType: "text/plain",
		})
	}
	return store.Batch{Source: "test", Spans: spans, Contents: contents}
}

func TestNoProgressOnRepeatedAnswer(t *testing.T) {
	st := openTemp(t)
	stuck := "I will now search the knowledge base for the answer."
	if err := st.WriteBatch(context.Background(), turnRun("r1", []string{stuck, stuck, stuck})); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	sweep(t, st)
	fs := runFindings(t, st, "r1")
	if len(fs) != 1 || fs[0].Type != "no_progress" || fs[0].SpanID != "r1-llmc" {
		t.Fatalf("findings = %+v", fs)
	}
}

func TestNoProgressIgnoresShortRepeats(t *testing.T) {
	st := openTemp(t)
	if err := st.WriteBatch(context.Background(), turnRun("r1", []string{"ok", "ok", "ok"})); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	sweep(t, st)
	if fs := runFindings(t, st, "r1"); len(fs) != 0 {
		t.Fatalf("short repeats flagged: %+v", fs)
	}
}

func TestNoProgressNeedsThreeTurns(t *testing.T) {
	st := openTemp(t)
	stuck := "I will now search the knowledge base for the answer."
	if err := st.WriteBatch(context.Background(), turnRun("r1", []string{stuck, stuck})); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	sweep(t, st)
	if fs := runFindings(t, st, "r1"); len(fs) != 0 {
		t.Fatalf("two turns flagged: %+v", fs)
	}
}

func TestNoProgressNotOnDistinctAnswers(t *testing.T) {
	st := openTemp(t)
	answers := []string{
		"First I will look at the calendar for open slots.",
		"Now I will draft the invitation to the whole team.",
		"Finally I will send the invite and confirm receipt.",
	}
	if err := st.WriteBatch(context.Background(), turnRun("r1", answers)); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	sweep(t, st)
	if fs := runFindings(t, st, "r1"); len(fs) != 0 {
		t.Fatalf("distinct answers flagged: %+v", fs)
	}
}
