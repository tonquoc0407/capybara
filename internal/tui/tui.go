// Package tui is the full-screen terminal interface: three panes, live updates.
package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tonquoc0407/capybara/internal/store"
	"github.com/tonquoc0407/capybara/internal/theme"
)

// About is what this process can tell the user about itself: the build, where
// it is writing, and which ways in are actually open. The empty state is built
// from it, so someone who just installed capybara is told what to do next.
type About struct {
	Version  string
	DBPath   string
	OTLP     string // endpoint accepting traces, empty when none bound
	OTLPErr  string // why nothing bound, or which transport was lost
	Watching string // session directory being tailed, empty when none
}

// Run starts the TUI and blocks until quit or ctx cancellation.
func Run(ctx context.Context, st *store.Store, th theme.Theme, about About, captureContent bool) error {
	ch, cancel := st.Subscribe()
	defer cancel()
	m := newApp(st, th, ch, captureContent)
	m.version, m.dbPath, m.about = about.Version, about.DBPath, about
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))
	if _, err := p.Run(); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}
