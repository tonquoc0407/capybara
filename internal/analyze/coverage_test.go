package analyze

import (
	"testing"

	"github.com/tonquoc0407/capybara/internal/store"
)

func TestSpanCoverageCountsKinds(t *testing.T) {
	spans := []store.Span{
		{Kind: store.KindLLM},
		{Kind: store.KindLLM},
		{Kind: store.KindTool},
		{Kind: store.KindOther},
	}
	cov := SpanCoverage(spans)
	if cov.Total != 4 || cov.ByKind[store.KindLLM] != 2 || cov.ByKind[store.KindTool] != 1 {
		t.Fatalf("coverage = %+v", cov)
	}
}

// A gen_ai span the mapper failed to type lands as other but still carries its
// namespace; coverage must surface it and mark it as an ingest gap, while a
// plain db span stays unmarked.
func TestSpanCoverageFlagsUnmappedAINamespace(t *testing.T) {
	spans := []store.Span{
		{Kind: store.KindOther, Attrs: store.Attrs{Raw: map[string]any{
			"gen_ai.request.model": "x", "gen_ai.operation.name": "rerank",
		}}},
		{Kind: store.KindOther, Attrs: store.Attrs{Raw: map[string]any{
			"db.system": "postgresql", "db.statement": "select 1",
		}}},
	}
	cov := SpanCoverage(spans)
	if len(cov.Prefixes) != 2 {
		t.Fatalf("prefixes = %+v", cov.Prefixes)
	}
	got := map[string]PrefixCount{}
	for _, p := range cov.Prefixes {
		got[p.Prefix] = p
	}
	if p := got["gen_ai"]; p.Count != 1 || !p.AI {
		t.Errorf("gen_ai = %+v, want count 1 AI true", p)
	}
	if p := got["db"]; p.Count != 1 || p.AI {
		t.Errorf("db = %+v, want count 1 AI false", p)
	}
}

// Typed spans never contribute to the unmapped-namespace tally, even the AI ones.
func TestSpanCoverageIgnoresTypedSpans(t *testing.T) {
	spans := []store.Span{
		{Kind: store.KindLLM, Attrs: store.Attrs{Raw: map[string]any{"gen_ai.request.model": "x"}}},
	}
	if cov := SpanCoverage(spans); len(cov.Prefixes) != 0 {
		t.Fatalf("typed span leaked into prefixes: %+v", cov.Prefixes)
	}
}
