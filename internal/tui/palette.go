package tui

import (
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type paletteAction struct {
	name string
	key  string
}

var paletteActions = []paletteAction{
	{"Connect an application", "i"},
	{"Search runs", "/"},
	{"Trace tree", "tree"},
	{"Cost waterfall", "w"},
	{"Context", "c"},
	{"Monitor", "m"},
	{"Blame", "b"},
	{"Compare runs", "d"},
	{"Export test", "t"},
	{"Full detail", "full"},
	{"Help", "?"},
}

func (m appModel) paletteMatches() []paletteAction {
	query := strings.ToLower(strings.TrimSpace(m.paletteQuery))
	var matches []paletteAction
	for _, action := range paletteActions {
		if strings.Contains(strings.ToLower(action.name), query) {
			matches = append(matches, action)
		}
	}
	return matches
}

func (m appModel) handlePaletteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+p":
		m.paletteOpen = false
		return m, nil
	case "up", "ctrl+k":
		m.paletteIndex = max(0, m.paletteIndex-1)
		return m, nil
	case "down", "ctrl+j":
		m.paletteIndex = min(len(m.paletteMatches())-1, m.paletteIndex+1)
		return m, nil
	case "backspace", "ctrl+h":
		if m.paletteQuery != "" {
			_, n := utf8.DecodeLastRuneInString(m.paletteQuery)
			m.paletteQuery = m.paletteQuery[:len(m.paletteQuery)-n]
			m.paletteIndex = 0
		}
		return m, nil
	case "enter":
		matches := m.paletteMatches()
		m.paletteOpen = false
		if len(matches) == 0 || m.paletteIndex < 0 || m.paletteIndex >= len(matches) {
			return m, nil
		}
		switch matches[m.paletteIndex].key {
		case "tree":
			m.mode, m.focus = viewTree, focusTree
			m.layout()
			return m, nil
		case "full":
			m.detailFull = true
			m.focus = focusDetail
			m.layout()
			return m, nil
		case "/":
			m.focus = focusRuns
		case "?":
			m.help.ShowAll = !m.help.ShowAll
			m.layout()
			return m, nil
		default:
			m.focus = focusTree
		}
		return m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(matches[m.paletteIndex].key)})
	}
	if msg.Type == tea.KeyRunes {
		m.paletteQuery += string(msg.Runes)
		m.paletteIndex = 0
	}
	return m, nil
}

func (m appModel) paletteView() string {
	var lines []string
	lines = append(lines, m.th.PaneTitle.Render("COMMANDS")+m.th.Dim.Render("  Esc close"), m.th.Accent.Render("⌕ ")+m.paletteQuery+"█", "")
	matches := m.paletteMatches()
	if len(matches) == 0 {
		lines = append(lines, m.th.Dim.Render("No matching commands"))
	}
	for i, action := range matches {
		prefix := "  "
		if i == m.paletteIndex {
			prefix = "> "
		}
		row := prefix + action.name
		if m.th.Name == "pupu" {
			if i == m.paletteIndex {
				lines = append(lines, m.th.Selected.Width(min(46, max(1, m.width-10))).Render(row))
			} else {
				lines = append(lines, m.th.Text.Render(row))
			}
		} else {
			lines = append(lines, row)
		}
	}
	box := m.th.BorderFocus.Border(lipgloss.RoundedBorder()).Padding(1, 2).
		Width(min(52, max(12, m.width-4))).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}
