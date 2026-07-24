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

// sprite is the capybara head, 22x18 pixels. Two pixel rows share one text row,
// so every row must be the same width, the count must stay even, and any
// two-pixel feature must start on an even row or it renders as two hairlines.
//
// The shape is what separates a capybara from cattle: ears standing proud of a
// flat crown rather than sticking out at the sides, eyes set high and far apart
// under a heavy brow, and a blunt muzzle block with two nostrils filling the
// lower face, with the chin below it.
const sprite = `
..DDDD..........DDDD..
.DDBBDDDDDDDDDDDDBBDD.
.DBIIBBBBBBBBBBBBIIBD.
.DBIIHHHHHHHHHHHHIIBD.
.DDBHDHHHHHHHHHHDHBDD.
.DBBBBBBHHHHHHBBBBBBD.
.DBBHEEHHHHHHHHEEHBBD.
.DBBHEEHHHHHHHHEEHBBD.
.DBBHHHHHHHHHHHHHHBBD.
.DBBHHHHHHHHHHHHHHBBD.
.DBBBBBBBBBBBBBBBBBBD.
.DBBBBBBBBBBBBBBBBBBD.
.DBBBBBBMNMMNMBBBBBBD.
.DBBBBBBMNMMNMBBBBBBD.
.DDBBBBBMMMMMMBBBBBDD.
..DDBBBBMMMMMMBBBBDD..
...DDDDPPPPPPPPDDDD...
......DPPPPPPPPD......`

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

// Coat is one theme's mascot colours, named rather than ordered because eight
// hex strings in a row is a mistake waiting to happen.
type Coat struct {
	Outline, Fur, Lit, Ear, Eye, Muzzle, Nostril, Chin string
}

func fur(c Coat) Palette {
	return Palette{
		'D': lipgloss.Color(c.Outline),
		'B': lipgloss.Color(c.Fur),
		'H': lipgloss.Color(c.Lit),
		'I': lipgloss.Color(c.Ear),
		'E': lipgloss.Color(c.Eye),
		'M': lipgloss.Color(c.Muzzle),
		'N': lipgloss.Color(c.Nostril),
		'P': lipgloss.Color(c.Chin),
	}
}
