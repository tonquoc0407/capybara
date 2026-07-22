package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tonquoc0407/capybara/internal/store"
	"github.com/tonquoc0407/capybara/internal/theme"
)

var base = time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)

func span(id, parent string, kind store.Kind, name string, start, dur int) store.Span {
	return store.Span{
		ID: id, RunID: "r1", ParentID: parent, Kind: kind, Name: name,
		StartedAt: base.Add(time.Duration(start) * time.Second),
		EndedAt:   base.Add(time.Duration(start+dur) * time.Second),
		Status:    "ok",
	}
}

func testTree() treeModel {
	m := newTree(theme.Bara())
	m.setSize(60, 20)
	m.setSpans([]store.Span{
		span("root", "", store.KindAgent, "agent_loop", 0, 10),
		span("llm1", "root", store.KindLLM, "chat", 1, 2),
		span("tool1", "llm1", store.KindTool, "search_db", 3, 1),
		span("tool2", "root", store.KindTool, "fetch_api", 5, 1),
	}, nil)
	return m
}

func press(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func rowIDs(m treeModel) []string {
	ids := make([]string, len(m.rows))
	for i, r := range m.rows {
		ids[i] = r.span.ID
	}
	return ids
}

func TestTreeFlattensDepthFirst(t *testing.T) {
	m := testTree()
	want := []string{"root", "llm1", "tool1", "tool2"}
	got := rowIDs(m)
	if len(got) != len(want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rows = %v, want %v", got, want)
		}
	}
	if m.rows[2].depth != 2 {
		t.Errorf("tool1 depth = %d, want 2", m.rows[2].depth)
	}
}

func TestTreeCollapseHidesSubtree(t *testing.T) {
	m := testTree()
	m.update(press("enter")) // collapse root
	if len(m.rows) != 1 {
		t.Fatalf("rows after collapse = %v", rowIDs(m))
	}
	m.update(press("enter"))
	if len(m.rows) != 4 {
		t.Fatalf("rows after expand = %v", rowIDs(m))
	}
}

func TestTreeOrphanRendersAsRoot(t *testing.T) {
	m := newTree(theme.Bara())
	m.setSize(60, 20)
	m.setSpans([]store.Span{span("lost", "gone", store.KindLLM, "chat", 0, 1)}, nil)
	if len(m.rows) != 1 || m.rows[0].depth != 0 {
		t.Fatalf("orphan rows = %v", rowIDs(m))
	}
}

func TestTreeFilterKeepsAncestors(t *testing.T) {
	m := testTree()
	m.update(press("f"))
	m.update(press("t")) // hide tools
	ids := rowIDs(m)
	if len(ids) != 2 || ids[0] != "root" || ids[1] != "llm1" {
		t.Fatalf("rows with tools hidden = %v", ids)
	}
	m.update(press("x")) // reset
	if len(m.rows) != 4 {
		t.Fatalf("rows after reset = %v", rowIDs(m))
	}
}

func TestTreeErrorsOnlyFilter(t *testing.T) {
	m := testTree()
	spans := []store.Span{
		span("root", "", store.KindAgent, "agent_loop", 0, 10),
		span("llm1", "root", store.KindLLM, "chat", 1, 2),
		span("tool1", "llm1", store.KindTool, "search_db", 3, 1),
	}
	spans[2].Status = "error"
	m.setSpans(spans, nil)
	m.update(press("f"))
	m.update(press("e"))
	ids := rowIDs(m)
	if len(ids) != 3 {
		t.Fatalf("error path rows = %v, want full ancestor chain", ids)
	}
	m.update(press("esc"))
	if m.filtering {
		t.Error("filtering still active after esc")
	}
}

func TestTreeSearchJumpsAndWraps(t *testing.T) {
	m := testTree()
	m.update(press("/"))
	for _, r := range "fetch" {
		m.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m.update(press("enter"))
	if got := m.selectedID(); got != "tool2" {
		t.Fatalf("selection after search = %q, want tool2", got)
	}
	m.update(press("n")) // single match: wraps back to itself
	if got := m.selectedID(); got != "tool2" {
		t.Fatalf("selection after n = %q, want tool2", got)
	}
}

func TestTreeSelectionSurvivesRefresh(t *testing.T) {
	m := testTree()
	m.update(press("j"))
	m.update(press("j"))
	if m.selectedID() != "tool1" {
		t.Fatalf("selection = %q, want tool1", m.selectedID())
	}
	spans := append([]store.Span{
		span("root", "", store.KindAgent, "agent_loop", 0, 10),
		span("llm1", "root", store.KindLLM, "chat", 1, 2),
		span("tool1", "llm1", store.KindTool, "search_db", 3, 1),
		span("tool2", "root", store.KindTool, "fetch_api", 5, 1),
	}, span("late", "root", store.KindTool, "late_tool", 6, 1))
	m.setSpans(spans, nil)
	if m.selectedID() != "tool1" {
		t.Errorf("selection after refresh = %q, want tool1", m.selectedID())
	}
	if len(m.rows) != 5 {
		t.Errorf("rows = %v", rowIDs(m))
	}
}
