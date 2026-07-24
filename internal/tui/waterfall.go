package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tonquoc0407/capybara/internal/store"
	"github.com/tonquoc0407/capybara/internal/theme"
)

// waterfallModel is the cost view: per-span cost/latency bars sorted by
// contribution. Bars are plain terminal cells; no plotting library.
type waterfallModel struct {
	th     theme.Theme
	width  int
	height int
	rows   []store.Span
	byCost bool
	sel    int
	offset int
}

func newWaterfall(th theme.Theme) waterfallModel {
	return waterfallModel{th: th}
}

func (m *waterfallModel) setSize(w, h int) {
	m.width, m.height = w, h
	m.clamp()
}

func (m *waterfallModel) setSpans(spans []store.Span) {
	sel := m.selectedID()
	m.rows = m.rows[:0]
	m.byCost = false
	for _, sp := range spans {
		if sp.EndedAt.IsZero() && sp.CostUSD == nil {
			continue
		}
		m.rows = append(m.rows, sp)
		if sp.CostUSD != nil {
			m.byCost = true
		}
	}
	sort.SliceStable(m.rows, func(i, j int) bool {
		return m.metric(m.rows[i]) > m.metric(m.rows[j])
	})
	m.sel = 0
	for i, sp := range m.rows {
		if sp.ID == sel {
			m.sel = i
			break
		}
	}
	m.clamp()
}

// metric is the contribution a row is sorted and scaled by: cost when the run
// has any, latency otherwise.
func (m *waterfallModel) metric(sp store.Span) float64 {
	if m.byCost {
		if sp.CostUSD == nil {
			return 0
		}
		return *sp.CostUSD
	}
	return sp.EndedAt.Sub(sp.StartedAt).Seconds()
}

func (m *waterfallModel) selectedID() string {
	if m.sel >= 0 && m.sel < len(m.rows) {
		return m.rows[m.sel].ID
	}
	return ""
}

func (m *waterfallModel) selected() (store.Span, bool) {
	if m.sel >= 0 && m.sel < len(m.rows) {
		return m.rows[m.sel], true
	}
	return store.Span{}, false
}

func (m *waterfallModel) update(msg tea.KeyMsg) {
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

func (m *waterfallModel) clamp() {
	if len(m.rows) == 0 {
		m.sel, m.offset = 0, 0
		return
	}
	m.sel = min(max(m.sel, 0), len(m.rows)-1)
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

func (m *waterfallModel) view() string {
	if len(m.rows) == 0 {
		return m.th.Dim.Render("no completed spans")
	}
	maxMetric := m.metric(m.rows[0])
	nameW := min(24, max(10, m.width/3))
	valueW := 14
	barW := max(4, m.width-nameW-valueW-2)
	var lines []string
	for i := m.offset; i < len(m.rows) && len(lines) < m.height; i++ {
		lines = append(lines, m.renderBar(m.rows[i], i, maxMetric, nameW, barW))
	}
	return strings.Join(lines, "\n")
}

func (m *waterfallModel) renderBar(sp store.Span, i int, maxMetric float64, nameW, barW int) string {
	name := sp.Name
	if sp.Kind == store.KindTool && sp.Attrs.ToolName != "" {
		name = sp.Attrs.ToolName
	}
	if sp.Kind == store.KindLLM && sp.Attrs.Model != "" {
		// Every turn of one session carries the same model, so the vendor
		// prefix is column width spent saying nothing.
		name = strings.ReplaceAll(name, sp.Attrs.Model, shortModel(sp.Attrs.Model))
	}
	name = truncate(name, nameW)
	fill := 0
	if maxMetric > 0 {
		fill = int(float64(barW) * m.metric(sp) / maxMetric)
	}
	fill = min(max(fill, 0), barW)
	if fill == 0 && m.metric(sp) > 0 {
		fill = 1
	}
	value := spanDuration(sp)
	if m.byCost {
		cost := "-"
		if sp.CostUSD != nil {
			cost = fmt.Sprintf("$%.4f", *sp.CostUSD)
		}
		value = cost + " " + value
	}
	line := fmt.Sprintf("%-*s %s%s %s",
		nameW, name,
		strings.Repeat("█", fill), strings.Repeat(" ", barW-fill),
		value)
	if i == m.sel {
		return m.th.Selected.Render(truncate(line, m.width))
	}
	style := m.th.Text
	if sp.Status == "error" {
		style = m.th.StatusErr
	}
	return style.Render(truncate(line, m.width))
}
