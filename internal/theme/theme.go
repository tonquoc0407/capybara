// Package theme holds the TUI color themes: flat lipgloss style structs.
package theme

import "github.com/charmbracelet/lipgloss"

// Theme is the complete style set for the TUI. Near-monochrome by charter:
// one accent color, status carried by characters plus weight.
type Theme struct {
	Name          string
	GlamourStyle  string
	MascotPalette Palette
	Header        lipgloss.Style
	HeaderInfo    lipgloss.Style
	Border        lipgloss.Style
	BorderFocus   lipgloss.Style
	PaneTitle     lipgloss.Style
	Text          lipgloss.Style
	Dim           lipgloss.Style
	Accent        lipgloss.Style
	Selected      lipgloss.Style
	Match         lipgloss.Style
	StatusOK      lipgloss.Style
	StatusErr     lipgloss.Style
	StatusWarn    lipgloss.Style
	StatusRun     lipgloss.Style
	StatusBar     lipgloss.Style
	HelpKey       lipgloss.Style
	HelpDesc      lipgloss.Style
}

// ByName returns the named theme, or false if it does not exist.
func ByName(name string) (Theme, bool) {
	for _, t := range All() {
		if t.Name == name {
			return t, true
		}
	}
	return Theme{}, false
}

// All returns the shipped themes; the first entry is the default.
func All() []Theme {
	return []Theme{Bara(), Mono(), Paper()}
}
