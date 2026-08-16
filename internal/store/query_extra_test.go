package store

import (
	"context"
	"testing"
)

// otherRunBatch is testBatch's fixture reattached to a second run: every span
// id must stay globally unique since spans are keyed by id alone, not by
// (run_id, id).
func otherRunBatch(runID string) Batch {
	b := testBatch()
	for i := range b.Spans {
		b.Spans[i].RunID = runID
		b.Spans[i].ID = runID + "-" + b.Spans[i].ID
	}
	b.Spans[1].ParentID, b.Spans[2].ParentID = runID+"-root", runID+"-root"
	b.Contents[0].SpanID, b.Contents[1].SpanID = runID+"-llm1", runID+"-llm1"
	return b
}

func TestAllFindingsAcrossRuns(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	b1 := testBatch()
	b1.Findings = []Finding{{RunID: "r1", Type: "parse_error", Severity: "warning", Detail: "{}"}}
	if err := s.WriteBatch(ctx, b1); err != nil {
		t.Fatalf("WriteBatch r1: %v", err)
	}
	b2 := otherRunBatch("r2")
	b2.Findings = []Finding{{RunID: "r2", Type: "tool_error", Severity: "warning", Detail: "{}"}}
	if err := s.WriteBatch(ctx, b2); err != nil {
		t.Fatalf("WriteBatch r2: %v", err)
	}
	findings, err := s.AllFindings(ctx)
	if err != nil {
		t.Fatalf("AllFindings: %v", err)
	}
	if len(findings) != 2 {
		t.Fatalf("got %d findings, want 2", len(findings))
	}
}

func TestTaintsRoundTrip(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.WriteBatch(ctx, testBatch()); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	edges := []Taint{{RunID: "r1", SpanID: "llm1", SourceSpanID: "tool1"}}
	if err := s.PutTaints(ctx, "r1", edges); err != nil {
		t.Fatalf("PutTaints: %v", err)
	}
	got, err := s.Taints(ctx, "r1")
	if err != nil {
		t.Fatalf("Taints: %v", err)
	}
	if len(got) != 1 || got[0].SpanID != "llm1" || got[0].SourceSpanID != "tool1" {
		t.Errorf("taints = %+v", got)
	}
	// A re-analysis with no more edges must not leave the old one behind.
	if err := s.PutTaints(ctx, "r1", nil); err != nil {
		t.Fatalf("PutTaints clear: %v", err)
	}
	if got, err = s.Taints(ctx, "r1"); err != nil || len(got) != 0 {
		t.Fatalf("taints after clear = %+v, err = %v", got, err)
	}
}

func TestContentsForRunGroupsBySpan(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.WriteBatch(ctx, testBatch()); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	grouped, err := s.ContentsForRun(ctx, "r1")
	if err != nil {
		t.Fatalf("ContentsForRun: %v", err)
	}
	if len(grouped["llm1"]) != 2 {
		t.Fatalf("llm1 contents = %+v", grouped["llm1"])
	}
	if _, ok := grouped["tool1"]; ok {
		t.Errorf("tool1 has no contents in the fixture, want it absent")
	}
}

func TestResolveRunID(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.WriteBatch(ctx, testBatch()); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	if got, err := s.ResolveRunID(ctx, "r1"); err != nil || got != "r1" {
		t.Fatalf("ResolveRunID(r1) = %q, %v", got, err)
	}
	if _, err := s.ResolveRunID(ctx, "nope"); err == nil {
		t.Error("expected an error for an unknown prefix")
	}
}

func TestResolveRunIDAmbiguousPrefix(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.WriteBatch(ctx, testBatch()); err != nil {
		t.Fatalf("WriteBatch r1: %v", err)
	}
	if err := s.WriteBatch(ctx, otherRunBatch("r12")); err != nil {
		t.Fatalf("WriteBatch r12: %v", err)
	}
	if _, err := s.ResolveRunID(ctx, "r1"); err == nil {
		t.Error("expected an ambiguous-prefix error")
	}
}

func TestToolCallsDistinguishRecordedFromUnrecorded(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	b := testBatch()
	b.Contents = append(b.Contents, Content{SpanID: "tool1", Role: "input", Seq: 0, Body: `{"q":"x"}`, MediaType: "application/json"})
	if err := s.WriteBatch(ctx, b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	calls, err := s.ToolCalls(ctx, "r1")
	if err != nil {
		t.Fatalf("ToolCalls: %v", err)
	}
	if len(calls) != 1 || !calls[0].Recorded || calls[0].Input != `{"q":"x"}` || calls[0].Tool != "search" {
		t.Errorf("calls = %+v", calls)
	}
}

func TestToolCallsUnrecordedInput(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.WriteBatch(ctx, testBatch()); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	calls, err := s.ToolCalls(ctx, "r1")
	if err != nil {
		t.Fatalf("ToolCalls: %v", err)
	}
	if len(calls) != 1 || calls[0].Recorded {
		t.Errorf("calls = %+v, want an unrecorded input", calls)
	}
}

func TestLLMCacheRoundTrip(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.WriteBatch(ctx, testBatch()); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	entries := []CachedLLM{{RunID: "r1", SpanID: "llm1", RequestHash: "h1", Response: `{"a":1}`}}
	if err := s.PutLLMCache(ctx, entries); err != nil {
		t.Fatalf("PutLLMCache: %v", err)
	}
	got, err := s.LLMCache(ctx, "r1")
	if err != nil {
		t.Fatalf("LLMCache: %v", err)
	}
	if len(got) != 1 || got[0].RequestHash != "h1" || got[0].Response != `{"a":1}` {
		t.Errorf("cache = %+v", got)
	}
	// Replaying the same span again must replace, not duplicate, the entry.
	entries[0].Response = `{"a":2}`
	if err := s.PutLLMCache(ctx, entries); err != nil {
		t.Fatalf("PutLLMCache replace: %v", err)
	}
	if got, err = s.LLMCache(ctx, "r1"); err != nil || len(got) != 1 || got[0].Response != `{"a":2}` {
		t.Fatalf("cache after replace = %+v, err = %v", got, err)
	}
}

func TestContentStatsSumsBodySizesByRole(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.WriteBatch(ctx, testBatch()); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	stats, err := s.ContentStats(ctx, "r1")
	if err != nil {
		t.Fatalf("ContentStats: %v", err)
	}
	if stats["llm1"]["user"] != int64(len("hi")) {
		t.Errorf("user size = %d, want %d", stats["llm1"]["user"], len("hi"))
	}
	if stats["llm1"]["assistant"] != int64(len(`{"a":1}`)) {
		t.Errorf("assistant size = %d", stats["llm1"]["assistant"])
	}
}

func TestAllSpansAcrossRuns(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.WriteBatch(ctx, testBatch()); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	spans, err := s.AllSpans(ctx)
	if err != nil {
		t.Fatalf("AllSpans: %v", err)
	}
	if len(spans) != 3 {
		t.Fatalf("got %d spans, want 3", len(spans))
	}
}
