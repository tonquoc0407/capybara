package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/store"
	"github.com/tonquoc0407/capybara/internal/theme"
)

// blameModel shows the tainted path from a run's final output to its root
// cause, one hop per row; selection drives the detail pane.
type blameModel struct {
	th     theme.Theme
	width  int
	height int
	chain  *analyze.BlameChain
	sel    int
	offset int
}

func newBlame(th theme.Theme) blameModel {
	return blameModel{th: th}
}

func (m *blameModel) setSize(w, h int) {
	m.width, m.height = w, h
	m.clamp()
}

func (m *blameModel) setChain(c *analyze.BlameChain) {
	m.chain = c
	m.sel, m.offset = 0, 0
	m.clamp()
}

func (m *blameModel) hops() []analyze.BlameHop {
	if m.chain == nil {
		return nil
	}
	return m.chain.Hops
}

func (m *blameModel) selected() (store.Span, bool) {
	hops := m.hops()
	if m.sel < 0 || m.sel >= len(hops) {
		return store.Span{}, false
	}
	return hops[m.sel].Span, true
}

func (m *blameModel) update(msg tea.KeyMsg) {
	switch msg.String() {
	case "j", "down":
		m.sel++
	case "k", "up":
		m.sel--
	case "g", "home":
		m.sel = 0
	case "G", "end":
		m.sel = len(m.hops()) - 1
	case "pgdown", "ctrl+d":
		m.sel += m.height / 2
	case "pgup", "ctrl+u":
		m.sel -= m.height / 2
	}
	m.clamp()
}

func (m *blameModel) clamp() {
	hops := m.hops()
	if len(hops) == 0 {
		m.sel, m.offset = 0, 0
		return
	}
	m.sel = min(max(m.sel, 0), len(hops)-1)
	if m.height <= 0 {
		return
	}
	if m.sel < m.offset {
		m.offset = m.sel
	}
	if m.sel >= m.offset+m.height {
		m.offset = m.sel - m.height + 1
	}
	m.offset = min(max(m.offset, 0), max(0, len(hops)-m.height))
}

func (m *blameModel) view() string {
	hops := m.hops()
	if len(hops) == 0 {
		return m.th.Dim.Render("final output carries no finding")
	}
	var lines []string
	for i := m.offset; i < len(hops) && len(lines) < m.height; i++ {
		lines = append(lines, m.renderRow(i))
	}
	return strings.Join(lines, "\n")
}

func (m *blameModel) renderRow(i int) string {
	hop := m.hops()[i]
	right := blameReasonText(hop)
	tags := []string{}
	if i == 0 {
		tags = append(tags, "output")
	}
	if hop.Root {
		tags = append(tags, "root")
	}
	if len(tags) > 0 {
		tag := "[" + strings.Join(tags, ",") + "]"
		if right == "" {
			right = tag
		} else {
			right = tag + " " + right
		}
	}
	nameW := max(10, m.width/2)
	label := strings.Repeat("  ", hop.Depth) + blameLabel(hop.Span)
	line := fmt.Sprintf("%-*s %s", nameW, truncate(label, nameW), right)
	style := m.th.Text
	switch {
	case i == m.sel:
		style = m.th.Selected
	case hop.Root:
		style = m.th.StatusErr
	case len(hop.Findings) > 0:
		style = m.th.StatusWarn
	}
	return style.Render(truncate(line, m.width))
}

func blameReasonText(hop analyze.BlameHop) string {
	if len(hop.Findings) == 0 {
		if hop.Span.Status == "error" {
			return "error"
		}
		return ""
	}
	parts := make([]string, 0, len(hop.Findings))
	for _, f := range hop.Findings {
		parts = append(parts, analyze.FindingSummary(f))
	}
	return strings.Join(parts, "; ")
}

func blameLabel(sp store.Span) string {
	name := sp.Name
	if sp.Attrs.ToolName != "" {
		name = sp.Attrs.ToolName
	}
	switch sp.Kind {
	case store.KindLLM, store.KindTool, store.KindRetrieval:
		return string(sp.Kind) + " " + name
	}
	return name
}
