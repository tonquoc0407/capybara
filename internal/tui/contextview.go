package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tonquoc0407/capybara/internal/store"
	"github.com/tonquoc0407/capybara/internal/theme"
)

// contextModel shows what occupies the context window over time: one stacked
// bar per llm turn, generalized from span contents. Segment proportions come
// from recorded content sizes, bar length from the turn's real token count.
// A sharp drop between turns marks an eviction.
const evictionDrop = 0.3

type contextRow struct {
	span    store.Span
	total   int64 // tokens_in of the turn; falls back to cumulative chars
	system  float64
	tools   float64
	history float64
	evicted bool
}

type contextModel struct {
	th     theme.Theme
	width  int
	height int
	rows   []contextRow
	sel    int
	offset int
}

func newContext(th theme.Theme) contextModel {
	return contextModel{th: th}
}

func (m *contextModel) setSize(w, h int) {
	m.width, m.height = w, h
	m.clamp()
}

func (m *contextModel) setData(spans []store.Span, stats map[string]map[string]int64) {
	sel := m.selectedID()
	m.rows = buildContextRows(spans, stats)
	m.sel = 0
	for i, r := range m.rows {
		if r.span.ID == sel {
			m.sel = i
			break
		}
	}
	m.clamp()
}

func buildContextRows(spans []store.Span, stats map[string]map[string]int64) []contextRow {
	var llms []store.Span
	for _, sp := range spans {
		if sp.Kind == store.KindLLM && !sp.EndedAt.IsZero() {
			llms = append(llms, sp)
		}
	}
	sort.SliceStable(llms, func(i, j int) bool {
		if !llms[i].EndedAt.Equal(llms[j].EndedAt) {
			return llms[i].EndedAt.Before(llms[j].EndedAt)
		}
		return llms[i].ID < llms[j].ID
	})
	type toolEnd struct {
		at   int64
		size int64
	}
	var toolEnds []toolEnd
	for _, sp := range spans {
		if sp.Kind != store.KindTool || sp.EndedAt.IsZero() {
			continue
		}
		if s := stats[sp.ID]; s != nil {
			toolEnds = append(toolEnds, toolEnd{at: sp.EndedAt.UnixNano(), size: s["output"] + s["input"]})
		}
	}
	sort.Slice(toolEnds, func(i, j int) bool { return toolEnds[i].at < toolEnds[j].at })
	rows := make([]contextRow, 0, len(llms))
	var sysChars, histChars int64
	toolIdx, toolSum := 0, int64(0)
	var prevTotal int64
	for _, sp := range llms {
		s := stats[sp.ID]
		sysChars += s["system"]
		for toolIdx < len(toolEnds) && toolEnds[toolIdx].at <= sp.EndedAt.UnixNano() {
			toolSum += toolEnds[toolIdx].size
			toolIdx++
		}
		charTotal := sysChars + histChars + toolSum
		total := sp.TokensIn
		if total == 0 {
			total = charTotal
		}
		row := contextRow{span: sp, total: total}
		if charTotal > 0 {
			row.system = float64(sysChars) / float64(charTotal)
			row.tools = float64(toolSum) / float64(charTotal)
			row.history = 1 - row.system - row.tools
		} else {
			row.history = 1
		}
		if prevTotal > 0 && float64(total) < float64(prevTotal)*(1-evictionDrop) {
			row.evicted = true
		}
		prevTotal = total
		rows = append(rows, row)
		// This turn's conversation becomes the next turn's history.
		histChars += s["user"] + s["assistant"] + s["thinking"]
	}
	return rows
}

func (m *contextModel) selectedID() string {
	if m.sel >= 0 && m.sel < len(m.rows) {
		return m.rows[m.sel].span.ID
	}
	return ""
}

func (m *contextModel) selected() (store.Span, bool) {
	if m.sel >= 0 && m.sel < len(m.rows) {
		return m.rows[m.sel].span, true
	}
	return store.Span{}, false
}

func (m *contextModel) update(msg tea.KeyMsg) {
	switch msg.String() {
	case "j", "down":
		m.sel++
	case "k", "up":
		m.sel--
	case "g", "home":
		m.sel = 0
	case "G", "end":
		m.sel = len(m.rows) - 1
	case "pgdown", "ctrl+d":
		m.sel += m.height / 2
	case "pgup", "ctrl+u":
		m.sel -= m.height / 2
	}
	m.clamp()
}

func (m *contextModel) clamp() {
	if len(m.rows) == 0 {
		m.sel, m.offset = 0, 0
		return
	}
	m.sel = min(max(m.sel, 0), len(m.rows)-1)
	h := m.viewHeight()
	if h <= 0 {
		return
	}
	if m.sel < m.offset {
		m.offset = m.sel
	}
	if m.sel >= m.offset+h {
		m.offset = m.sel - h + 1
	}
	m.offset = min(max(m.offset, 0), max(0, len(m.rows)-h))
}

// viewHeight leaves the last line for the legend.
func (m *contextModel) viewHeight() int {
	return max(0, m.height-1)
}

func (m *contextModel) view() string {
	if len(m.rows) == 0 {
		return m.th.Dim.Render("no llm turns")
	}
	var maxTotal int64 = 1
	for _, r := range m.rows {
		maxTotal = max(maxTotal, r.total)
	}
	labelW := 8
	valueW := 11
	barW := max(4, m.width-labelW-valueW-2)
	lines := make([]string, 0, m.height)
	for i := m.offset; i < len(m.rows) && len(lines) < m.viewHeight(); i++ {
		lines = append(lines, m.renderRow(i, maxTotal, labelW, barW))
	}
	for len(lines) < m.viewHeight() {
		lines = append(lines, "")
	}
	legend := m.th.Dim.Render(truncate("█ system  ▓ tool results  ░ history", m.width))
	return strings.Join(append(lines, legend), "\n")
}

func (m *contextModel) renderRow(i int, maxTotal int64, labelW, barW int) string {
	r := m.rows[i]
	width := int(float64(barW) * float64(r.total) / float64(maxTotal))
	width = min(max(width, 1), barW)
	sysW := int(r.system * float64(width))
	toolW := int(r.tools * float64(width))
	histW := width - sysW - toolW
	label := r.span.EndedAt.Format("15:04:05")
	bar := strings.Repeat("█", sysW) + strings.Repeat("▓", toolW) + strings.Repeat("░", histW)
	value := fmt.Sprintf("%d tok", r.total)
	line := fmt.Sprintf("%-*s %s%s %s", labelW, label, bar, strings.Repeat(" ", barW-width), value)
	if r.evicted {
		line = fmt.Sprintf("%-*s %s", labelW, label,
			truncate("← context shrank; content evicted "+bar, m.width-labelW-1))
	}
	if i == m.sel {
		return m.th.Selected.Render(truncate(line, m.width))
	}
	if r.evicted {
		return m.th.StatusWarn.Render(truncate(line, m.width))
	}
	return m.th.Text.Render(truncate(line, m.width))
}
