package theme

import "github.com/charmbracelet/lipgloss"

// Pupu is the default warm-dark workspace theme, built around the logo's fur.
func Pupu() Theme {
	var (
		canvas  = lipgloss.Color("#17120F")
		surface = lipgloss.Color("#241C18")
		chosen  = lipgloss.Color("#54372A")
		accent  = lipgloss.Color("#E8AD78")
		text    = lipgloss.Color("#F4E8D9")
		dim     = lipgloss.Color("#B5A08F")
		faint   = lipgloss.Color("#73594B")
		errFg   = lipgloss.Color("#F18D83")
		okFg    = lipgloss.Color("#A9C49C")
	)
	return Theme{
		Name:         "pupu",
		GlamourStyle: "dark",
		MascotPalette: fur(Coat{
			Outline: "#2b1e15", Fur: "#906B4C", Lit: "#A7825D", Ear: "#704C34",
			Eye: "#140e0a", Muzzle: "#866040", Nostril: "#241812", Chin: "#BA956D",
		}),
		Shell:        lipgloss.NewStyle().Foreground(text).Background(canvas),
		TabActive:    lipgloss.NewStyle().Foreground(canvas).Background(accent).Bold(true).Padding(0, 1),
		TabIdle:      lipgloss.NewStyle().Foreground(dim).Background(canvas).Padding(0, 1),
		SelectedMeta: lipgloss.NewStyle().Foreground(accent).Background(chosen),
		Header:       lipgloss.NewStyle().Foreground(canvas).Background(accent).Bold(true),
		HeaderInfo:   lipgloss.NewStyle().Foreground(text).Background(canvas),
		Border:       lipgloss.NewStyle().Foreground(faint).Background(surface),
		BorderFocus:  lipgloss.NewStyle().Foreground(accent).Background(surface),
		PaneTitle:    lipgloss.NewStyle().Foreground(accent).Background(surface).Bold(true),
		Text:         lipgloss.NewStyle().Foreground(text).Background(surface),
		Dim:          lipgloss.NewStyle().Foreground(dim).Background(surface),
		Accent:       lipgloss.NewStyle().Foreground(accent).Background(surface).Bold(true),
		Selected:     lipgloss.NewStyle().Foreground(text).Background(chosen).Bold(true),
		Match:        lipgloss.NewStyle().Foreground(accent).Background(surface).Underline(true),
		StatusOK:     lipgloss.NewStyle().Foreground(okFg).Background(surface),
		StatusErr:    lipgloss.NewStyle().Foreground(errFg).Background(surface).Bold(true),
		StatusWarn:   lipgloss.NewStyle().Foreground(accent).Background(surface).Bold(true),
		StatusRun:    lipgloss.NewStyle().Foreground(okFg).Background(surface),
		StatusBar:    lipgloss.NewStyle().Foreground(dim).Background(canvas),
		HelpKey:      lipgloss.NewStyle().Foreground(accent).Bold(true),
		HelpDesc:     lipgloss.NewStyle().Foreground(dim),
	}
}
