package tui

import (
	"strings"
	"testing"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/store"
	"github.com/tonquoc0407/capybara/internal/theme"
)

func costOf(v float64) *float64 { return &v }

func TestWaterfallSortsByCost(t *testing.T) {
	m := newWaterfall(theme.Bara())
	m.setSize(70, 10)
	cheap := span("llm1", "root", store.KindLLM, "chat small", 0, 1)
	cheap.CostUSD = costOf(0.01)
	dear := span("llm2", "root", store.KindLLM, "chat big", 2, 1)
	dear.CostUSD = costOf(0.50)
	tool := span("tool1", "root", store.KindTool, "search", 4, 8)
	m.setSpans([]store.Span{cheap, dear, tool})
	if m.rows[0].ID != "llm2" {
		t.Fatalf("rows[0] = %s, want llm2 (highest cost)", m.rows[0].ID)
	}
	out := plainView2(m.view())
	if !strings.Contains(out, "$0.5000") || !strings.Contains(out, "█") {
		t.Errorf("waterfall view:\n%s", out)
	}
	if sp, ok := m.selected(); !ok || sp.ID != "llm2" {
		t.Errorf("initial selection = %v", sp.ID)
	}
	m.update(press("j"))
	if sp, _ := m.selected(); sp.ID != "llm1" {
		t.Errorf("selection after j = %s", sp.ID)
	}
}

func TestWaterfallFallsBackToLatency(t *testing.T) {
	m := newWaterfall(theme.Bara())
	m.setSize(70, 10)
	fast := span("t1", "root", store.KindTool, "quick", 0, 1)
	slow := span("t2", "root", store.KindTool, "slow", 2, 9)
	m.setSpans([]store.Span{fast, slow})
	if m.rows[0].ID != "t2" {
		t.Fatalf("rows[0] = %s, want t2 (slowest)", m.rows[0].ID)
	}
	if !strings.Contains(plainView2(m.view()), "9.0s") {
		t.Errorf("view lacks latency:\n%s", m.view())
	}
}

func contextFixture() ([]store.Span, map[string]map[string]int64) {
	llm1 := span("llm1", "root", store.KindLLM, "chat", 0, 1)
	llm1.TokensIn = 1000
	tool := span("tool1", "llm1", store.KindTool, "search", 2, 1)
	llm2 := span("llm2", "root", store.KindLLM, "chat", 4, 1)
	llm2.TokensIn = 5000
	llm3 := span("llm3", "root", store.KindLLM, "chat", 6, 1)
	llm3.TokensIn = 1200 // sharp drop: eviction
	stats := map[string]map[string]int64{
		"llm1":  {"system": 400, "user": 100, "assistant": 200},
		"tool1": {"input": 50, "output": 950},
		"llm2":  {"user": 80, "assistant": 120},
		"llm3":  {"user": 30},
	}
	return []store.Span{llm1, tool, llm2, llm3}, stats
}

func TestContextRowsProportionsAndEviction(t *testing.T) {
	spans, stats := contextFixture()
	rows := buildContextRows(spans, stats)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	if rows[0].total != 1000 || rows[1].total != 5000 {
		t.Errorf("totals = %d, %d", rows[0].total, rows[1].total)
	}
	// Turn 2's context: system 400, history 300, tool 1000 chars.
	if rows[1].tools < 0.5 || rows[1].system < 0.2 {
		t.Errorf("turn2 proportions = %+v", rows[1])
	}
	if rows[0].evicted || rows[1].evicted || !rows[2].evicted {
		t.Errorf("eviction flags = %v %v %v", rows[0].evicted, rows[1].evicted, rows[2].evicted)
	}
}

func TestContextRowsAddCachedReadsBack(t *testing.T) {
	spans, stats := contextFixture()
	spans[0].Attrs.Raw = map[string]any{
		"usage": map[string]any{"cache_read_input_tokens": float64(9000)},
	}
	rows := buildContextRows(spans, stats)
	if rows[0].total != 10000 {
		t.Errorf("row total = %d, want 10000 (1000 new + 9000 cached)", rows[0].total)
	}
}

func TestContextViewRenders(t *testing.T) {
	m := newContext(theme.Bara())
	m.setSize(70, 12)
	spans, stats := contextFixture()
	m.setData(spans, stats)
	out := plainView2(m.view())
	for _, want := range []string{"5000 tok", "evicted", "█ system"} {
		if !strings.Contains(out, want) {
			t.Errorf("context view missing %q:\n%s", want, out)
		}
	}
	if sp, ok := m.selected(); !ok || sp.ID != "llm1" {
		t.Errorf("initial selection = %v", sp)
	}
}

func TestEconomicsFindingSummaries(t *testing.T) {
	loop := store.Finding{Type: "loop", Detail: `{"pattern":["search_db","fetch"],"n":2}`}
	if got := analyze.FindingSummary(loop); got != "tool loop: search_db, fetch" {
		t.Errorf("loop summary = %q", got)
	}
	spike := store.Finding{Type: "cost_spike", Detail: `{"tokens":40000,"baseline":2000}`}
	if got := analyze.FindingSummary(spike); got != "token spike: 40000 vs 2000 baseline" {
		t.Errorf("spike summary = %q", got)
	}
}

func plainView2(s string) string {
	return ansiRE.ReplaceAllString(s, "")
}
