package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tonquoc0407/capybara/internal/theme"
)

func TestPaletteFiltersAndRunsCommand(t *testing.T) {
	m := newApp(seededStore(t), theme.Bara(), nil, true)
	m.width, m.height = 90, 30
	m.layout()
	model, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = model.(appModel)
	if !m.paletteOpen || !strings.Contains(m.View(), "Search runs") {
		t.Fatal("palette did not open")
	}
	model, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("waterfall")})
	m = model.(appModel)
	if matches := m.paletteMatches(); len(matches) != 1 || matches[0].name != "Cost waterfall" {
		t.Fatalf("unexpected matches: %+v", matches)
	}
	model, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(appModel)
	if m.paletteOpen || m.mode != viewWaterfall {
		t.Fatal("palette command did not select waterfall")
	}
}

func TestPaletteFullDetailCanClose(t *testing.T) {
	m := newApp(seededStore(t), theme.Bara(), nil, true)
	m.width, m.height = 60, 24
	m.layout()
	model, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = model.(appModel)
	model, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("full detail")})
	m = model.(appModel)
	model, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(appModel)
	if !m.detailFull || !strings.Contains(m.View(), "Esc to close") {
		t.Fatal("full detail did not open")
	}
	model, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = model.(appModel)
	if m.detailFull {
		t.Fatal("Esc did not close full detail")
	}
}
