package theme

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Mood is what the mascot reflects: the worst thing currently on screen.
type Mood int

// Moods, worst last: nothing wrong, findings recorded, something failed.
const (
	Calm Mood = iota
	Alert
	Concerned
)

// Face is the mascot in two cells, for the status bar. A drawn sprite cannot
// work there: one text row is two pixels tall, too few to read as a face.
func Face(m Mood) string {
	switch m {
	case Alert:
		return "oo"
	case Concerned:
		return "><"
	default:
		return "^^"
	}
}

// Palette colours the mascot sprite. A legend character with no entry is
// transparent and shows the terminal through.
type Palette map[byte]lipgloss.Color

// sprite is the capybara head, 24x16 pixels. Two pixel rows share one text row,
// so every row must be the same width and the count must stay even.
const sprite = `
........................
.....DDDDDDDDDDDDDD.....
...DDHHHHHHHHHHHHHHDD...
..DIIDHHHHHHHHHHHHDIID..
..DIIDHHHHHHHHHHHHDIID..
..DDIDBBBBBBBBBBBBDIDD..
...DDBBBBBBBBBBBBBBDD...
...DBBDDDBBBBBBDDDBBD...
...DBBEEEBBBBBBEEEBBD...
...DBBBBBBBBBBBBBBBBD...
...DBBMMMMMMMMMMMMBBD...
...DBMMMMMMMMMMMMMMBD...
...DBMMNNMMMMMMNNMMBD...
...DBMMMMMMMMMMMMMMBD...
....DDMMMMMMMMMMMMDD....
......DDDDDDDDDDDD......`

// Mascot draws the sprite as half-block rows: the upper half-block carries the
// top pixel as foreground, the lower one the bottom pixel as background.
func (t Theme) Mascot() string {
	rows := strings.Split(strings.Trim(sprite, "\n"), "\n")
	lines := make([]string, 0, len(rows)/2)
	for y := 0; y < len(rows)-1; y += 2 {
		lines = append(lines, t.mascotRow(rows[y], rows[y+1]))
	}
	return strings.Join(lines, "\n")
}

func (t Theme) mascotRow(top, bottom string) string {
	var b strings.Builder
	for x := range len(top) {
		tc, hasTop := t.MascotPalette[top[x]]
		bc, hasBottom := t.MascotPalette[bottom[x]]
		switch {
		case hasTop && hasBottom:
			b.WriteString(lipgloss.NewStyle().Foreground(tc).Background(bc).Render("▀"))
		case hasTop:
			b.WriteString(lipgloss.NewStyle().Foreground(tc).Render("▀"))
		case hasBottom:
			b.WriteString(lipgloss.NewStyle().Foreground(bc).Render("▄"))
		default:
			b.WriteString(" ")
		}
	}
	return b.String()
}

// fur is the shared sprite palette: outline, fur, lit fur, muzzle, eye,
// nostril, inner ear.
func fur(outline, body, lit, muzzle, eye, nostril, ear string) Palette {
	return Palette{
		'D': lipgloss.Color(outline),
		'B': lipgloss.Color(body),
		'H': lipgloss.Color(lit),
		'M': lipgloss.Color(muzzle),
		'E': lipgloss.Color(eye),
		'N': lipgloss.Color(nostril),
		'I': lipgloss.Color(ear),
	}
}
