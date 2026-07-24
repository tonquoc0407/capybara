package theme

import "github.com/charmbracelet/lipgloss"

// Mono drops the accent hue to grey and leans on weight instead. Red and amber
// stay: they are status codes, not palette.
func Mono() Theme {
	var (
		accent = lipgloss.Color("255")
		text   = lipgloss.Color("250")
		dim    = lipgloss.Color("245")
		faint  = lipgloss.Color("239")
		errFg  = lipgloss.Color("203")
		warnFg = lipgloss.Color("178")
	)
	return Theme{
		Name:          "mono",
		GlamourStyle:  "ascii",
		MascotPalette: fur("#1c1c1c", "#6c6c6c", "#767676", "#8a8a8a", "#0a0a0a", "#303030", "#4e4e4e"),
		Header:        lipgloss.NewStyle().Foreground(accent).Bold(true),
		HeaderInfo:    lipgloss.NewStyle().Foreground(text),
		Border:        lipgloss.NewStyle().Foreground(faint),
		BorderFocus:   lipgloss.NewStyle().Foreground(accent),
		PaneTitle:     lipgloss.NewStyle().Foreground(dim).Bold(true),
		Text:          lipgloss.NewStyle().Foreground(text),
		Dim:           lipgloss.NewStyle().Foreground(dim),
		Accent:        lipgloss.NewStyle().Foreground(accent).Bold(true),
		Selected:      lipgloss.NewStyle().Foreground(accent).Bold(true).Reverse(true),
		Match:         lipgloss.NewStyle().Foreground(accent).Underline(true),
		StatusOK:      lipgloss.NewStyle().Foreground(dim),
		StatusErr:     lipgloss.NewStyle().Foreground(errFg).Bold(true),
		StatusWarn:    lipgloss.NewStyle().Foreground(warnFg),
		StatusRun:     lipgloss.NewStyle().Foreground(text),
		StatusBar:     lipgloss.NewStyle().Foreground(dim),
		HelpKey:       lipgloss.NewStyle().Foreground(text),
		HelpDesc:      lipgloss.NewStyle().Foreground(dim),
	}
}
