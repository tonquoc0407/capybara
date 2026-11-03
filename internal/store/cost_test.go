package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestCostProvenanceAndUsage(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "cost.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sp := Span{ID: "llm", RunID: "run", Kind: KindLLM, Name: "chat", Status: "ok",
		StartedAt: time.Now(), EndedAt: time.Now(), TokensIn: 100, TokensOut: 20}
	if err := st.WriteBatch(ctx, Batch{Source: "test", RunContexts: map[string]RunContext{"run": {Project: "checkout"}}, Spans: []Span{sp}}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSpanEstimates(ctx, map[string]SpanEstimate{"llm": {
		CostUSD: 0.05, Provenance: CostProvenance{Kind: "estimate", PriceSource: "test-table",
			PriceVersion: "abc", UsageSource: "observed token totals"},
	}}); err != nil {
		t.Fatal(err)
	}
	ps, err := st.CostProvenanceForRun(ctx, "run")
	if err != nil || len(ps) != 1 || ps[0].PriceVersion != "abc" || ps[0].CheckedAt.IsZero() {
		t.Fatalf("provenance: %+v, %v", ps, err)
	}
	u, err := st.UsageByProject(ctx)
	if err != nil || len(u) != 1 || u[0].Project != "checkout" || u[0].EstimatedUSD != 0.05 || u[0].UnpricedLLMSpans != 0 {
		t.Fatalf("usage: %+v, %v", u, err)
	}
}
