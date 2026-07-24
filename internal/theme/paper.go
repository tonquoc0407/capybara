package theme

import "github.com/charmbracelet/lipgloss"

// Paper is bara for a light terminal: the same amber accent darkened until it
// reads on white, with the greys inverted.
func Paper() Theme {
	var (
		accent = lipgloss.Color("130")
		text   = lipgloss.Color("236")
		dim    = lipgloss.Color("243")
		faint  = lipgloss.Color("250")
		errFg  = lipgloss.Color("124")
	)
	return Theme{
		Name:         "paper",
		GlamourStyle: "light",
		MascotPalette: fur(Coat{
			Outline: "#3a291c", Fur: "#8a6642", Lit: "#96714d", Ear: "#553926",
			Eye: "#140e0a", Muzzle: "#5f4229", Nostril: "#241812", Chin: "#a87f57",
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
