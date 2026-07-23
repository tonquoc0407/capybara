package tui

import (
	"fmt"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/store"
)

// findingLines is the expanded form for the detail pane: summary plus the
// schema diff or evidence.
func findingLines(f store.Finding) []string {
	d := analyze.ParseDetail(f)
	lines := []string{f.Type + ": " + analyze.FindingSummary(f)}
	for _, field := range d.Missing {
		lines = append(lines, "  missing: "+field)
	}
	for _, r := range d.Retyped {
		lines = append(lines, fmt.Sprintf("  %s: want %s, got %s", r.Field, r.Want, r.Got))
	}
	if f.Type == "improvised" {
		lines = append(lines, "  cause: "+d.Cause, "  evidence: "+d.Evidence)
	}
	if f.Type == "parse_error" && d.Error != "" {
		lines = append(lines, "  "+d.Error)
	}
	return lines
}
