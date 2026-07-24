package theme

import (
	"strings"
	"testing"
)

// The sprite is read two rows at a time, so a ragged or odd-numbered grid would
// silently drop or misalign pixels.
func TestSpriteGridPairsUpEvenly(t *testing.T) {
	rows := strings.Split(strings.Trim(sprite, "\n"), "\n")
	if len(rows)%2 != 0 {
		t.Fatalf("sprite has %d rows, want an even count", len(rows))
	}
	for i, row := range rows {
		if len(row) != len(rows[0]) {
			t.Errorf("row %d is %d wide, want %d", i, len(row), len(rows[0]))
		}
	}
}

func TestEveryThemeColoursTheWholeSprite(t *testing.T) {
	legend := map[byte]bool{}
	for _, row := range strings.Split(strings.Trim(sprite, "\n"), "\n") {
		for i := range len(row) {
			if row[i] != '.' {
				legend[row[i]] = true
			}
		}
	}
	for _, th := range All() {
		for ch := range legend {
			if _, ok := th.MascotPalette[ch]; !ok {
				t.Errorf("theme %s has no colour for %q", th.Name, ch)
			}
		}
	}
}

func TestMascotRendersOneRowPerPixelPair(t *testing.T) {
	rows := strings.Split(strings.Trim(sprite, "\n"), "\n")
	lines := strings.Split(Bara().Mascot(), "\n")
	if len(lines) != len(rows)/2 {
		t.Fatalf("mascot has %d lines, want %d", len(lines), len(rows)/2)
	}
}

func TestFaceIsTwoCellsPerMood(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range []Mood{Calm, Alert, Concerned} {
		f := Face(m)
		if len([]rune(f)) != 2 {
			t.Errorf("face %q for mood %d is not two cells", f, m)
		}
		if seen[f] {
			t.Errorf("mood %d reuses face %q", m, f)
		}
		seen[f] = true
	}
}
