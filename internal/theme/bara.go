package theme

import "github.com/charmbracelet/lipgloss"

// Bara is the default theme: warm dark, amber accent.
func Bara() Theme {
	var (
		accent = lipgloss.Color("172") // warm amber
		text   = lipgloss.Color("252")
		dim    = lipgloss.Color("243")
		faint  = lipgloss.Color("238")
		errFg  = lipgloss.Color("203")
	)
	return Theme{
		Name:         "bara",
		GlamourStyle: "dark",
		MascotPalette: fur(Coat{
			Outline: "#2b1e15", Fur: "#8a6642", Lit: "#9b7550", Ear: "#63452f",
			Eye: "#140e0a", Muzzle: "#7a583a", Nostril: "#241812", Chin: "#b08a63",
		}),
		Header:      lipgloss.NewStyle().Foreground(accent).Bold(true),
		HeaderInfo:  lipgloss.NewStyle().Foreground(text),
		Border:      lipgloss.NewStyle().Foreground(faint),
		BorderFocus: lipgloss.NewStyle().Foreground(accent),
		PaneTitle:   lipgloss.NewStyle().Foreground(dim).Bold(true),
		Text:        lipgloss.NewStyle().Foreground(text),
		Dim:         lipgloss.NewStyle().Foreground(dim),
		Accent:      lipgloss.NewStyle().Foreground(accent),
		Selected:    lipgloss.NewStyle().Foreground(accent).Bold(true).Reverse(true),
		Match:       lipgloss.NewStyle().Foreground(accent).Underline(true),
		StatusOK:    lipgloss.NewStyle().Foreground(dim),
		StatusErr:   lipgloss.NewStyle().Foreground(errFg).Bold(true),
		StatusWarn:  lipgloss.NewStyle().Foreground(accent),
		StatusRun:   lipgloss.NewStyle().Foreground(text),
		StatusBar:   lipgloss.NewStyle().Foreground(dim),
		HelpKey:     lipgloss.NewStyle().Foreground(text),
		HelpDesc:    lipgloss.NewStyle().Foreground(dim),
	}
}
