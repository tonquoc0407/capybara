package tui

import (
	"strings"
	"testing"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/store"
	"github.com/tonquoc0407/capybara/internal/theme"
)

func blameFixture() *analyze.BlameChain {
	tool := span("tool1", "llm1", store.KindTool, "search_db", 3, 1)
	tool.Status = "error"
	tool.Attrs.ToolName = "search_db"
	return &analyze.BlameChain{RunID: "r1", Hops: []analyze.BlameHop{
		{Span: span("root", "", store.KindAgent, "agent_loop", 0, 10)},
		{
			Span:     span("llm2", "root", store.KindLLM, "chat", 4, 2),
			Findings: []store.Finding{{Type: "improvised", Detail: `{"tool":"search_db"}`}},
		},
		{Span: tool, Root: true},
	}}
}

func TestBlameViewRendersChain(t *testing.T) {
	m := newBlame(theme.Bara())
	m.setSize(70, 10)
	m.setChain(blameFixture())
	out := plainView2(m.view())
	for _, want := range []string{"agent_loop", "output", "improvised after search_db failure", "tool search_db", "root"} {
		if !strings.Contains(out, want) {
			t.Errorf("blame view missing %q:\n%s", want, out)
		}
	}
	if sp, ok := m.selected(); !ok || sp.ID != "root" {
		t.Errorf("initial selection = %v", sp)
	}
	m.update(press("G"))
	if sp, _ := m.selected(); sp.ID != "tool1" {
		t.Errorf("selection after G = %s", sp.ID)
	}
}

func TestBlameViewEmptyChain(t *testing.T) {
	m := newBlame(theme.Bara())
	m.setSize(70, 10)
	m.setChain(&analyze.BlameChain{RunID: "r1"})
	if !strings.Contains(plainView2(m.view()), "no finding") {
		t.Errorf("empty blame view = %q", m.view())
	}
	if _, ok := m.selected(); ok {
		t.Error("empty chain reports a selection")
	}
}
