package tui

import (
	"strings"
	"testing"

	"github.com/tonquoc0407/capybara/internal/theme"
)

func TestTUIStartsInWorkspaceAndAdaptsToNarrowTerminal(t *testing.T) {
	m := newApp(nil, theme.Bara(), nil, false)
	m.width, m.height = 60, 28
	m.layout()
	view := plain(m.View())
	if !strings.Contains(view, "getting started") || strings.Contains(view, "trace debugger for agents") {
		t.Fatalf("narrow startup did not show workspace: %q", view)
	}
	m.focus = focusDetail
	m.layout()
	if view := plain(m.View()); !strings.Contains(view, "detail") || strings.Contains(view, "getting started") {
		t.Fatalf("narrow detail pane: %q", view)
	}
	m.width = 90
	m.focus = focusTree
	m.layout()
	view = plain(m.View())
	if !strings.Contains(view, "runs") || !strings.Contains(view, "getting started") || strings.Contains(view, "detail") {
		t.Fatalf("two-pane layout: %q", view)
	}
}
