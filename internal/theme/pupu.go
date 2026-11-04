package theme

import "github.com/charmbracelet/lipgloss"

// Pupu is the default warm-dark workspace theme, built around the logo's fur.
func Pupu() Theme {
	var (
		canvas  = lipgloss.Color("#15110F")
		surface = lipgloss.Color("#241A16")
		chosen  = lipgloss.Color("#A65332")
		accent  = lipgloss.Color("#FFB16A")
		text    = lipgloss.Color("#FFF0DE")
		dim     = lipgloss.Color("#C7AB98")
		faint   = lipgloss.Color("#745346")
		errFg   = lipgloss.Color("#FF7775")
		okFg    = lipgloss.Color("#A7DCBA")
		toolFg  = lipgloss.Color("#88D8D0")
		retrFg  = lipgloss.Color("#D1B7F0")
	)
	return Theme{
		Name:         "pupu",
		GlamourStyle: "dark",
		MascotPalette: fur(Coat{
			Outline: "#2b1e15", Fur: "#906B4C", Lit: "#A7825D", Ear: "#704C34",
			Eye: "#140e0a", Muzzle: "#866040", Nostril: "#241812", Chin: "#BA956D",
		}),
		Shell:         lipgloss.NewStyle().Foreground(text).Background(canvas),
		TabActive:     lipgloss.NewStyle().Foreground(canvas).Background(accent).Bold(true).Padding(0, 1),
		TabIdle:       lipgloss.NewStyle().Foreground(dim).Background(canvas).Padding(0, 1),
		SelectedMeta:  lipgloss.NewStyle().Foreground(text).Background(chosen),
		KindLLM:       lipgloss.NewStyle().Foreground(accent).Background(surface),
		KindTool:      lipgloss.NewStyle().Foreground(toolFg).Background(surface),
		KindRetrieval: lipgloss.NewStyle().Foreground(retrFg).Background(surface),
		Header:        lipgloss.NewStyle().Foreground(canvas).Background(accent).Bold(true),
		HeaderInfo:    lipgloss.NewStyle().Foreground(text).Background(canvas),
		Border:        lipgloss.NewStyle().Foreground(faint).Background(surface),
		BorderFocus:   lipgloss.NewStyle().Foreground(accent).Background(surface),
		PaneTitle:     lipgloss.NewStyle().Foreground(accent).Background(surface).Bold(true),
		Text:          lipgloss.NewStyle().Foreground(text).Background(surface),
		Dim:           lipgloss.NewStyle().Foreground(dim).Background(surface),
		Accent:        lipgloss.NewStyle().Foreground(accent).Background(surface).Bold(true),
		Selected:      lipgloss.NewStyle().Foreground(text).Background(chosen).Bold(true),
		Match:         lipgloss.NewStyle().Foreground(accent).Background(surface).Underline(true),
		StatusOK:      lipgloss.NewStyle().Foreground(okFg).Background(surface),
		StatusErr:     lipgloss.NewStyle().Foreground(errFg).Background(surface).Bold(true),
		StatusWarn:    lipgloss.NewStyle().Foreground(accent).Background(surface).Bold(true),
		StatusRun:     lipgloss.NewStyle().Foreground(okFg).Background(surface),
		StatusBar:     lipgloss.NewStyle().Foreground(dim).Background(canvas),
		HelpKey:       lipgloss.NewStyle().Foreground(accent).Bold(true),
		HelpDesc:      lipgloss.NewStyle().Foreground(dim),
	}
}
