package tui

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/tonquoc0407/capybara/internal/theme"
)

func splashModel(t *testing.T) appModel {
	t.Helper()
	st := seededStore(t)
	ch, cancel := st.Subscribe()
	t.Cleanup(cancel)
	m := newApp(st, theme.Bara(), ch, true)
	m.version, m.dbPath = "v0.1.0", "/tmp/capybara.db"
	m.width, m.height = 96, 24
	return m
}

func TestSplashNamesTheBuildAndTheDatabase(t *testing.T) {
	view := splashModel(t).splashView()
	for _, want := range []string{"capybara", "v0.1.0", "/tmp/capybara.db"} {
		if !strings.Contains(view, want) {
			t.Errorf("splash missing %q", want)
		}
	}
}

// Centring the composed lines one at a time would ragged the sprite, because
// the rows carrying text beside them are wider. Every sprite row must land on
// the same column.
func TestSplashKeepsTheSpriteEdgeStraight(t *testing.T) {
	m := splashModel(t)
	view := strings.Split(plain(m.splashView()), "\n")
	cols := make([]int, 0, 8)
	for _, row := range strings.Split(plain(m.th.Mascot()), "\n") {
		for _, line := range view {
			if i := strings.Index(line, row); i >= 0 {
				cols = append(cols, len([]rune(line[:i])))
				break
			}
		}
	}
	if len(cols) != 8 {
		t.Fatalf("located %d of 8 sprite rows", len(cols))
	}
	for _, c := range cols[1:] {
		if c != cols[0] {
			t.Fatalf("sprite rows start at columns %v, want one column", cols)
		}
	}
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func plain(s string) string { return ansi.ReplaceAllString(s, "") }

func TestStatusBarWearsTheWorstMood(t *testing.T) {
	m := splashModel(t)
	if got := m.mood(); got != theme.Calm {
		t.Errorf("clean run mood = %v, want Calm", got)
	}
	m.lastErr = errors.New("open db: permission denied")
	if got := m.mood(); got != theme.Concerned {
		t.Errorf("mood after an error = %v, want Concerned", got)
	}
	if !strings.Contains(m.statusView(), theme.Face(theme.Concerned)) {
		t.Error("status bar does not carry the mascot face")
	}
}
