package store

import (
	"context"
	"errors"
	"testing"
)

func TestReplaceAnalysisRejectsChangedRun(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	sp := Span{ID: "s", RunID: "r", Kind: KindTool, Name: "fetch",
		StartedAt: t0, EndedAt: t0.Add(1), Status: "ok"}
	if err := st.WriteBatch(ctx, Batch{Source: "test", Spans: []Span{sp}}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := st.Spans(ctx, "r")
	if err != nil {
		t.Fatal(err)
	}
	sp.Status = "error"
	if err := st.WriteBatch(ctx, Batch{Source: "test", Spans: []Span{sp}}); err != nil {
		t.Fatal(err)
	}
	finding := Finding{RunID: "r", SpanID: "s", Type: "tool_error", Severity: "warning",
		Detail: `{}`, Origin: "analyzer_span"}
	if err := st.ReplaceAnalysis(ctx, "r", snapshot, snapshot, []Finding{finding}, nil); !errors.Is(err, ErrAnalysisStale) {
		t.Fatalf("ReplaceAnalysis = %v, want stale snapshot", err)
	}
	latest, err := st.Spans(ctx, "r")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceAnalysis(ctx, "r", latest, snapshot, []Finding{finding}, nil); !errors.Is(err, ErrAnalysisStale) {
		t.Fatalf("ReplaceAnalysis with stale fresh span = %v, want stale snapshot", err)
	}
	if findings, err := st.Findings(ctx, "r"); err != nil || len(findings) != 0 {
		t.Fatalf("stale finding written: %+v, %v", findings, err)
	}
}
