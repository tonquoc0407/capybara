package analyze

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

func TestSweepRecordsPricingProvenance(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "prices.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sp := store.Span{
		ID: "llm", RunID: "run", Kind: store.KindLLM, Name: "chat",
		StartedAt: time.Now(), EndedAt: time.Now(), Status: "ok",
		TokensIn: 1000, TokensOut: 100, Attrs: store.Attrs{Model: "gpt-4o"},
	}
	if err := st.WriteBatch(ctx, store.Batch{Source: "test", Spans: []store.Span{sp}}); err != nil {
		t.Fatal(err)
	}
	an, err := New(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := an.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	ps, err := st.CostProvenanceForRun(ctx, "run")
	if err != nil || len(ps) != 1 || ps[0].Kind != "estimate" || ps[0].PriceVersion == "" || ps[0].PriceEffectiveAt != nil {
		t.Fatalf("pricing provenance: %+v, %v", ps, err)
	}
}

func TestAutomaticPricingPreservesExplicitAmountsAndRefreshesLateUsage(t *testing.T) {
	ctx := context.Background()
	st := openTemp(t)
	spans := []store.Span{}
	for _, id := range []string{"automatic", "provider", "proxy", "legacy"} {
		sp := store.Span{
			ID: id, RunID: id + "-run", Kind: store.KindLLM, Name: "chat", Status: "ok",
			StartedAt: t0, EndedAt: t0.Add(time.Second), TokensIn: 1000, TokensOut: 100, Attrs: store.Attrs{Model: "gpt-4o-mini"},
		}
		if id == "legacy" {
			cost := 5.0
			sp.CostUSD = &cost
		}
		spans = append(spans, sp)
	}
	if err := st.WriteBatch(ctx, store.Batch{Source: "test", Spans: spans}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSpanEstimates(ctx, map[string]store.SpanEstimate{
		"provider": {CostUSD: 0, Provenance: store.CostProvenance{Kind: "provider", PriceSource: "provider receipt", PriceVersion: "receipt-1"}},
		"proxy":    {CostUSD: 2, Provenance: store.CostProvenance{Kind: "estimate", PriceSource: "proxy configuration", PriceVersion: "custom-rates"}},
	}); err != nil {
		t.Fatal(err)
	}
	a := newAnalyzer(t, st)
	if err := a.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	automatic, err := st.GetRun(ctx, "automatic-run")
	if err != nil || automatic.CostUSD == nil || *automatic.CostUSD <= 0 {
		t.Fatalf("automatic cost: %+v, %v", automatic, err)
	}
	oldCost := *automatic.CostUSD
	late := spans[0]
	late.TokensIn *= 2
	if err := st.WriteBatch(ctx, store.Batch{Source: "late-usage", Spans: []store.Span{late}}); err != nil {
		t.Fatal(err)
	}
	if err := a.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	automatic, err = st.GetRun(ctx, "automatic-run")
	if err != nil || automatic.CostUSD == nil || *automatic.CostUSD <= oldCost {
		t.Fatalf("late usage kept a stale cost: %+v, %v", automatic, err)
	}
	for id, want := range map[string]float64{"provider": 0, "proxy": 2, "legacy": 5} {
		r, err := st.GetRun(ctx, id+"-run")
		if err != nil || r.CostUSD == nil || *r.CostUSD != want {
			t.Fatalf("%s explicit cost replaced: %+v, %v", id, r, err)
		}
	}
	for id, version := range map[string]string{"provider": "receipt-1", "proxy": "custom-rates"} {
		p, err := st.CostProvenanceForRun(ctx, id+"-run")
		if err != nil || len(p) != 1 || p[0].PriceVersion != version {
			t.Fatalf("%s provenance replaced: %+v, %v", id, p, err)
		}
	}
}
