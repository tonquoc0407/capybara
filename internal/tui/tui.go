// Package tui is the full-screen terminal interface: three panes, live updates.
package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tonquoc0407/capybara/internal/store"
	"github.com/tonquoc0407/capybara/internal/theme"
)

// About is what the splash screen says about this build.
type About struct {
	Version string
	DBPath  string
}

// Run starts the TUI and blocks until quit or ctx cancellation.
func Run(ctx context.Context, st *store.Store, th theme.Theme, about About, captureContent bool) error {
	ch, cancel := st.Subscribe()
	defer cancel()
	m := newApp(st, th, ch, captureContent)
	m.version, m.dbPath = about.Version, about.DBPath
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))
	if _, err := p.Run(); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}
