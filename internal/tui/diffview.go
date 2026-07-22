package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/theme"
)

// diffModel shows two aligned runs: delta columns per step, divergences
// marked, the first one annotated.
type diffRow struct {
	step analyze.DiffStep
	note string
}

type diffModel struct {
	th     theme.Theme
	width  int
	height int
	diff   *analyze.RunDiff
	rows   []diffRow
	sel    int
	offset int
}

func newDiff(th theme.Theme) diffModel {
	return diffModel{th: th}
}

func (m *diffModel) setSize(w, h int) {
	m.width, m.height = w, h
	m.clamp()
}

func (m *diffModel) setDiff(d *analyze.RunDiff) {
	m.diff = d
	m.rows = m.rows[:0]
	for i, step := range d.Steps {
		m.rows = append(m.rows, diffRow{step: step})
		if i == d.FirstDivergence {
			m.rows = append(m.rows, diffRow{step: step, note: "first divergence"})
		}
	}
	m.sel = max(0, d.FirstDivergence)
	m.clamp()
}

func (m *diffModel) selected() (analyze.DiffStep, bool) {
	if m.sel >= 0 && m.sel < len(m.rows) && m.rows[m.sel].note == "" {
		return m.rows[m.sel].step, true
	}
	return analyze.DiffStep{}, false
}

func (m *diffModel) update(msg tea.KeyMsg) {
	switch msg.String() {
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "g", "home":
		m.sel = 0
	case "G", "end":
		m.sel = len(m.rows) - 1
	case "pgdown", "ctrl+d":
		m.move(m.height / 2)
	case "pgup", "ctrl+u":
		m.move(-m.height / 2)
	}
	m.clamp()
}

func (m *diffModel) move(delta int) {
	dir := 1
	if delta < 0 {
		dir, delta = -1, -delta
	}
	for range delta {
		next := m.sel + dir
		for next >= 0 && next < len(m.rows) && m.rows[next].note != "" {
			next += dir
		}
		if next < 0 || next >= len(m.rows) {
			break
		}
		m.sel = next
	}
}

func (m *diffModel) clamp() {
	if len(m.rows) == 0 {
		m.sel, m.offset = 0, 0
		return
	}
	m.sel = min(max(m.sel, 0), len(m.rows)-1)
	for m.sel > 0 && m.rows[m.sel].note != "" {
		m.sel--
	}
	if m.height <= 0 {
		return
	}
	if m.sel < m.offset {
		m.offset = m.sel
	}
	if m.sel >= m.offset+m.height {
		m.offset = m.sel - m.height + 1
	}
	m.offset = min(max(m.offset, 0), max(0, len(m.rows)-m.height))
}

func (m *diffModel) view() string {
	if m.diff == nil || len(m.rows) == 0 {
		return m.th.Dim.Render("no diff")
	}
	var lines []string
	for i := m.offset; i < len(m.rows) && len(lines) < m.height; i++ {
		lines = append(lines, m.renderRow(i))
	}
	return strings.Join(lines, "\n")
}

func (m *diffModel) renderRow(i int) string {
	r := m.rows[i]
	if r.note != "" {
		return m.th.StatusWarn.Render(truncate("    ^ "+r.note, m.width))
	}
	step := r.step
	mark := " "
	if step.Diverged {
		mark = "*"
	}
	nameW := max(10, m.width/2-8)
	var right string
	switch {
	case step.A == nil:
		right = "only in " + shortID(m.diff.RunB)
	case step.B == nil:
		right = "only in " + shortID(m.diff.RunA)
	default:
		right = fmt.Sprintf("tok %-7s %-9s %s",
			signedTokens(step.DTokens()), signedDelta(step.DCost()),
			signedDur(step.DLatency()))
	}
	line := fmt.Sprintf("%s %-*s %s", mark, nameW, truncate(step.StepName(), nameW), right)
	switch {
	case i == m.sel:
		return m.th.Selected.Render(truncate(line, m.width))
	case step.Diverged:
		return m.th.StatusWarn.Render(truncate(line, m.width))
	}
	return m.th.Text.Render(truncate(line, m.width))
}

func signedTokens(v int64) string {
	if v > 0 {
		return fmt.Sprintf("+%d", v)
	}
	return fmt.Sprintf("%d", v)
}

func signedDelta(v *float64) string {
	if v == nil {
		return ""
	}
	if *v >= 0 {
		return fmt.Sprintf("$+%.4f", *v)
	}
	return fmt.Sprintf("$%.4f", *v)
}

func signedDur(d time.Duration) string {
	sign := "+"
	if d < 0 {
		sign, d = "-", -d
	}
	switch {
	case d < time.Second:
		return fmt.Sprintf("%s%dms", sign, d.Milliseconds())
	default:
		return fmt.Sprintf("%s%.1fs", sign, d.Seconds())
	}
}
