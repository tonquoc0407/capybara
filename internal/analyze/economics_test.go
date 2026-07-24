package analyze

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

func toolCall(id, tool, input string) store.ToolCall {
	return store.ToolCall{SpanID: id, Tool: tool, Input: input, Recorded: true}
}

// A database recorded with -no-content, or fed by an instrumentor that does not
// capture arguments, has no input to compare. Hashing the missing input made
// every call to one tool look identical: a real session of 191 spans reported
// nine loops that way, all of them different files being read.
func TestNoLoopWhenArgumentsWereNeverRecorded(t *testing.T) {
	var calls []store.ToolCall
	for i := range 10 {
		calls = append(calls, store.ToolCall{SpanID: fmt.Sprintf("s%d", i), Tool: "read"})
	}
	if fs := loopFindings("r1", calls); len(fs) != 0 {
		t.Fatalf("unrecorded arguments flagged as loop: %+v", fs)
	}
}

func TestLoopOnRepeatedIdenticalCalls(t *testing.T) {
	calls := []store.ToolCall{
		toolCall("s1", "search", `{"q":"a"}`),
		toolCall("s2", "search", `{"q":"a"}`),
		toolCall("s3", "search", `{"q":"a"}`),
		toolCall("s4", "search", `{"q":"a"}`),
	}
	fs := loopFindings("r1", calls)
	if len(fs) != 1 || fs[0].Type != "loop" || fs[0].SpanID != "s1" {
		t.Fatalf("findings = %+v", fs)
	}
	if !strings.Contains(fs[0].Detail, "search") {
		t.Errorf("detail = %s", fs[0].Detail)
	}
}

func TestNoLoopOnDistinctInputs(t *testing.T) {
	var calls []store.ToolCall
	for i := range 10 {
		calls = append(calls, toolCall(fmt.Sprintf("s%d", i), "read", fmt.Sprintf(`{"file":"f%d"}`, i)))
	}
	if fs := loopFindings("r1", calls); len(fs) != 0 {
		t.Fatalf("distinct inputs flagged as loop: %+v", fs)
	}
}

func TestNoLoopOnThreeRetries(t *testing.T) {
	calls := []store.ToolCall{
		toolCall("s1", "fetch", `{"u":"x"}`),
		toolCall("s2", "fetch", `{"u":"x"}`),
		toolCall("s3", "fetch", `{"u":"x"}`),
	}
	if fs := loopFindings("r1", calls); len(fs) != 0 {
		t.Fatalf("three retries flagged as loop: %+v", fs)
	}
}

func TestLoopOnRepeatedBigram(t *testing.T) {
	var calls []store.ToolCall
	for i := range 3 {
		calls = append(calls, toolCall(fmt.Sprintf("a%d", i), "search", `{"q":"x"}`))
		calls = append(calls, toolCall(fmt.Sprintf("b%d", i), "fetch", `{"u":"y"}`))
	}
	fs := loopFindings("r1", calls)
	if len(fs) != 1 || fs[0].SpanID != "a0" {
		t.Fatalf("findings = %+v", fs)
	}
	if !strings.Contains(fs[0].Detail, `"n":2`) {
		t.Errorf("detail = %s", fs[0].Detail)
	}
}

func llmSpan(runID string, i int, tokens int64) store.Span {
	return store.Span{
		ID: fmt.Sprintf("%s-llm%d", runID, i), RunID: runID, Kind: store.KindLLM,
		Name: "chat", StartedAt: t0.Add(time.Duration(i) * time.Minute),
		EndedAt: t0.Add(time.Duration(i)*time.Minute + time.Second),
		Status:  "ok", TokensIn: tokens / 2, TokensOut: tokens - tokens/2,
	}
}

func TestSpikeAgainstRollingBaseline(t *testing.T) {
	spans := []store.Span{
		llmSpan("r1", 0, 2000), llmSpan("r1", 1, 2200), llmSpan("r1", 2, 1800),
		llmSpan("r1", 3, 2000), llmSpan("r1", 4, 40000),
	}
	fs := spikeFindings(spans)
	if len(fs) != 1 || fs[0].Type != "cost_spike" || fs[0].SpanID != "r1-llm4" {
		t.Fatalf("findings = %+v", fs)
	}
	if !strings.Contains(fs[0].Detail, `"tokens":40000`) {
		t.Errorf("detail = %s", fs[0].Detail)
	}
}

func TestNoSpikeOnSteadyBurn(t *testing.T) {
	var spans []store.Span
	for i := range 8 {
		spans = append(spans, llmSpan("r1", i, 2000+int64(i)*300))
	}
	if fs := spikeFindings(spans); len(fs) != 0 {
		t.Fatalf("steady burn flagged: %+v", fs)
	}
}

func TestNoSpikeBelowFloor(t *testing.T) {
	tiny := []store.Span{
		llmSpan("r1", 0, 50), llmSpan("r1", 1, 60), llmSpan("r1", 2, 40),
		llmSpan("r1", 3, 55), llmSpan("r1", 4, 600),
	}
	if fs := spikeFindings(tiny); len(fs) != 0 {
		t.Fatalf("tiny turns flagged: %+v", fs)
	}
	// A real turn can run many times the baseline and still be too small to
	// bill anyone's attention.
	ordinary := []store.Span{
		llmSpan("r2", 0, 2000), llmSpan("r2", 1, 2200), llmSpan("r2", 2, 1800),
		llmSpan("r2", 3, 2000), llmSpan("r2", 4, 15000),
	}
	if fs := spikeFindings(ordinary); len(fs) != 0 {
		t.Fatalf("7x the baseline but under the floor was flagged: %+v", fs)
	}
}

func TestSweepPricesKnownModels(t *testing.T) {
	st := openTemp(t)
	sp := llmSpan("r1", 0, 0)
	sp.TokensIn, sp.TokensOut = 1_000_000, 100_000
	sp.Attrs.Model = "gpt-4o-2024-11-20"
	unknown := llmSpan("r1", 1, 3000)
	unknown.Attrs.Model = "mystery-model"
	b := store.Batch{Source: "test", Spans: []store.Span{sp, unknown}}
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	sweep(t, st)
	spans, err := st.Spans(context.Background(), "r1")
	if err != nil {
		t.Fatalf("Spans: %v", err)
	}
	byID := map[string]store.Span{}
	for _, s := range spans {
		byID[s.ID] = s
	}
	priced := byID["r1-llm0"]
	if priced.CostUSD == nil || *priced.CostUSD < 3.49 || *priced.CostUSD > 3.51 {
		t.Fatalf("gpt-4o cost = %v, want 3.50", priced.CostUSD)
	}
	if byID["r1-llm1"].CostUSD != nil {
		t.Errorf("unknown model got a price: %v", *byID["r1-llm1"].CostUSD)
	}
	runs, err := st.ListRuns(context.Background())
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if runs[0].CostUSD == nil || *runs[0].CostUSD < 3.49 {
		t.Errorf("run cost = %v", runs[0].CostUSD)
	}
}

// A newer release in a priced family shares its prefix but not its rates.
func TestSweepLeavesNewerVersionsUnpriced(t *testing.T) {
	st := openTemp(t)
	newer := llmSpan("r1", 0, 0)
	newer.TokensIn, newer.TokensOut = 1_000_000, 100_000
	newer.Attrs.Model = "claude-opus-4-9"
	dated := llmSpan("r1", 1, 0)
	dated.TokensIn, dated.TokensOut = 1_000_000, 100_000
	dated.Attrs.Model = "claude-opus-4-5-20251101"
	b := store.Batch{Source: "test", Spans: []store.Span{newer, dated}}
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	sweep(t, st)
	spans, err := st.Spans(context.Background(), "r1")
	if err != nil {
		t.Fatalf("Spans: %v", err)
	}
	byID := map[string]store.Span{}
	for _, s := range spans {
		byID[s.ID] = s
	}
	if c := byID["r1-llm0"].CostUSD; c != nil {
		t.Errorf("claude-opus-4-9 inherited claude-opus-4 rates: %v", *c)
	}
	if byID["r1-llm1"].CostUSD == nil {
		t.Error("a dated release of a priced model went unpriced")
	}
}

func TestCachedUsagePricing(t *testing.T) {
	st := openTemp(t)
	sp := llmSpan("r1", 0, 0)
	sp.Attrs.Model = "claude-haiku-4-5"
	sp.Attrs.Raw = map[string]any{"usage": map[string]any{
		"input_tokens":                float64(1000),
		"output_tokens":               float64(2000),
		"cache_creation_input_tokens": float64(100_000),
		"cache_read_input_tokens":     float64(1_000_000),
	}}
	b := store.Batch{Source: "test", Spans: []store.Span{sp}}
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	sweep(t, st)
	spans, err := st.Spans(context.Background(), "r1")
	if err != nil {
		t.Fatalf("Spans: %v", err)
	}
	// 0.001*1 + 0.002*5 + 0.1*1.25 + 1.0*0.1 = 0.236
	if spans[0].CostUSD == nil || *spans[0].CostUSD < 0.2355 || *spans[0].CostUSD > 0.2365 {
		t.Fatalf("cached cost = %v, want 0.236", spans[0].CostUSD)
	}
}

// Google's SDKs report the model as a resource name, so a Gemini run through
// LangChain arrives as models/gemini-2.5-flash and used to be left unpriced.
func TestGeminiResourceNameIsPriced(t *testing.T) {
	p, err := loadPricing("")
	if err != nil {
		t.Fatalf("loadPricing: %v", err)
	}
	bare, ok := p.lookup("gemini-2.5-flash")
	if !ok {
		t.Fatal("gemini-2.5-flash is not in the table")
	}
	full, ok := p.lookup("models/gemini-2.5-flash")
	if !ok || full != bare {
		t.Errorf("models/ prefix priced as %+v, want %+v", full, bare)
	}
}
