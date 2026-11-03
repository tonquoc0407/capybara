package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/tonquoc0407/capybara/internal/theme"
)

func (m appModel) splashView() string {
	lines := []string{
		m.th.Header.Render("capybara") + "  " + m.th.Dim.Render(m.version),
		m.th.Text.Render("trace debugger for agents"),
		"",
		m.th.Dim.Render(m.dbPath),
	}
	// Joining two blocks keeps the sprite's left edge straight; centring the
	// composed lines one by one would ragged it, since they differ in width.
	info := lipgloss.NewStyle().PaddingLeft(4).Render(strings.Join(lines, "\n"))
	body := lipgloss.JoinHorizontal(lipgloss.Center, m.th.Mascot(), info)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)
}

// mood is the worst thing currently on screen, which is what the mascot wears.
func (m appModel) mood() theme.Mood {
	if m.lastErr != nil {
		return theme.Concerned
	}
	if run, ok := m.runs.selectedRun(); ok {
		if run.Status == "error" {
			return theme.Concerned
		}
		if run.Findings > 0 {
			return theme.Alert
		}
	}
	return theme.Calm
}
