package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
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

func TestPupuWorkspaceFitsTerminal(t *testing.T) {
	for _, width := range []int{40, 60, 90, 110, 140} {
		m := newApp(nil, theme.Pupu(), nil, false)
		m.width, m.height = width, 28
		m.layout()
		view := m.View()
		if !strings.Contains(plain(view), "capybara") || !strings.Contains(plain(view), "CONNECT") && width >= 100 {
			t.Fatalf("width %d missing navigation", width)
		}
		if width >= 100 && (!strings.Contains(plain(view), "ACTIVE RUN") || !strings.Contains(plain(view), "FINDINGS")) {
			t.Fatalf("width %d missing workspace rail or status strip", width)
		}
		if got := lipgloss.Height(view); got > m.height {
			t.Fatalf("width %d rendered %d rows into %d", width, got, m.height)
		}
		for _, line := range strings.Split(view, "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("width %d rendered line of %d cells", width, got)
			}
		}
		m.connectOpen = true
		m.about.OTLP = "http://127.0.0.1:4318"
		connectView := m.View()
		if got := lipgloss.Height(connectView); got > m.height {
			t.Fatalf("connect width %d rendered %d rows into %d", width, got, m.height)
		}
		for _, line := range strings.Split(connectView, "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("connect width %d rendered line of %d cells", width, got)
			}
		}
		m.connectOpen = false
		m.mode = viewMonitor
		if !strings.Contains(plain(m.pupuStatusView()), "MONITOR") || !strings.Contains(plain(m.pupuStatusView()), "esc trace") {
			t.Fatalf("monitor mode missing from footer at width %d", width)
		}
	}
}
