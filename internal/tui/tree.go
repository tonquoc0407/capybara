package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/store"
	"github.com/tonquoc0407/capybara/internal/theme"
)

type treeRow struct {
	span  store.Span
	depth int
	kids  bool
	note  string // finding annotation line; not selectable
}

// treeModel is the span tree: custom widget, no bubble fits a collapsible tree.
type treeModel struct {
	th        theme.Theme
	width     int
	height    int
	spans     []store.Span
	findings  map[string][]store.Finding
	byParent  map[string][]store.Span
	parentOf  map[string]string
	collapsed map[string]bool
	rows      []treeRow
	sel       int
	offset    int
	hideKinds map[store.Kind]bool
	errOnly   bool
	filtering bool
	searching bool
	input     textinput.Model
	query     string
}

func newTree(th theme.Theme) treeModel {
	in := textinput.New()
	in.Prompt = "/"
	in.CharLimit = 64
	return treeModel{
		th:        th,
		collapsed: make(map[string]bool),
		hideKinds: make(map[store.Kind]bool),
		input:     in,
	}
}

func (m *treeModel) setSize(w, h int) {
	m.width, m.height = w, h
	m.input.Width = max(1, w-2)
	m.clamp()
}

func (m *treeModel) setSpans(spans []store.Span, findings map[string][]store.Finding) {
	sel := m.selectedID()
	m.spans = spans
	m.findings = findings
	m.rebuild()
	if sel != "" {
		m.selectByID(sel)
	}
	m.clamp()
}

func (m *treeModel) selectedID() string {
	if m.sel >= 0 && m.sel < len(m.rows) && m.rows[m.sel].note == "" {
		return m.rows[m.sel].span.ID
	}
	return ""
}

func (m *treeModel) selected() (store.Span, bool) {
	if m.sel >= 0 && m.sel < len(m.rows) && m.rows[m.sel].note == "" {
		return m.rows[m.sel].span, true
	}
	return store.Span{}, false
}

func (m *treeModel) selectByID(id string) {
	for i, r := range m.rows {
		if r.span.ID == id && r.note == "" {
			m.sel = i
			return
		}
	}
}

func (m *treeModel) rebuild() {
	m.byParent = make(map[string][]store.Span)
	m.parentOf = make(map[string]string)
	ids := make(map[string]bool, len(m.spans))
	for _, sp := range m.spans {
		ids[sp.ID] = true
	}
	for _, sp := range m.spans {
		parent := sp.ParentID
		if parent != "" && !ids[parent] {
			parent = "" // orphans render as roots; never drop data
		}
		m.byParent[parent] = append(m.byParent[parent], sp)
		m.parentOf[sp.ID] = parent
	}
	visible := m.visibleSet()
	m.rows = m.rows[:0]
	var walk func(parent string, depth int)
	walk = func(parent string, depth int) {
		for _, sp := range m.byParent[parent] {
			if !visible[sp.ID] {
				continue
			}
			kids := len(m.byParent[sp.ID]) > 0
			m.rows = append(m.rows, treeRow{span: sp, depth: depth, kids: kids})
			for i, f := range m.findings[sp.ID] {
				if i == 2 {
					more := len(m.findings[sp.ID]) - i
					m.rows = append(m.rows, treeRow{
						span: sp, depth: depth, note: fmt.Sprintf("+%d more findings", more),
					})
					break
				}
				m.rows = append(m.rows, treeRow{span: sp, depth: depth, note: analyze.FindingSummary(f)})
			}
			if kids && !m.collapsed[sp.ID] {
				walk(sp.ID, depth+1)
			}
		}
	}
	walk("", 0)
	m.clamp()
}

// visibleSet keeps spans passing the filter plus their ancestors for context.
func (m *treeModel) visibleSet() map[string]bool {
	visible := make(map[string]bool, len(m.spans))
	if !m.filterActive() {
		for _, sp := range m.spans {
			visible[sp.ID] = true
		}
		return visible
	}
	for _, sp := range m.spans {
		if m.hideKinds[sp.Kind] || (m.errOnly && sp.Status != "error") {
			continue
		}
		for id := sp.ID; id != "" && !visible[id]; id = m.parentOf[id] {
			visible[id] = true
		}
	}
	return visible
}

func (m *treeModel) filterActive() bool {
	return m.errOnly || len(m.hideKinds) > 0
}

func (m *treeModel) typing() bool {
	return m.searching || m.filtering
}

func (m *treeModel) update(msg tea.KeyMsg) tea.Cmd {
	if m.searching {
		return m.updateSearch(msg)
	}
	if m.filtering {
		m.updateFilter(msg)
		return nil
	}
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
		m.move(m.viewHeight() / 2)
	case "pgup", "ctrl+u":
		m.move(-m.viewHeight() / 2)
	case "enter", " ":
		if r, ok := m.selected(); ok && m.rows[m.sel].kids {
			m.collapsed[r.ID] = !m.collapsed[r.ID]
			m.rebuild()
			m.selectByID(r.ID)
		}
	case "/":
		m.searching = true
		m.input.SetValue(m.query)
		return m.input.Focus()
	case "n":
		m.jump(1)
	case "N":
		m.jump(-1)
	case "f":
		m.filtering = true
	}
	m.clamp()
	return nil
}

func (m *treeModel) updateSearch(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.searching = false
		m.query = ""
		m.input.Blur()
	case "enter":
		m.searching = false
		m.query = m.input.Value()
		m.input.Blur()
		if sp, ok := m.selected(); ok && m.query != "" && !m.matches(sp) {
			m.jump(1)
		}
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return cmd
	}
	return nil
}

func (m *treeModel) updateFilter(msg tea.KeyMsg) {
	toggle := func(k store.Kind) {
		if m.hideKinds[k] {
			delete(m.hideKinds, k)
		} else {
			m.hideKinds[k] = true
		}
	}
	switch msg.String() {
	case "a":
		toggle(store.KindAgent)
	case "l":
		toggle(store.KindLLM)
	case "t":
		toggle(store.KindTool)
	case "r":
		toggle(store.KindRetrieval)
	case "o":
		toggle(store.KindOther)
	case "e":
		m.errOnly = !m.errOnly
	case "x":
		m.hideKinds = make(map[store.Kind]bool)
		m.errOnly = false
	case "f", "esc", "enter":
		m.filtering = false
	default:
		return
	}
	m.rebuild()
}

// move steps the selection over note rows, which are annotations, not targets.
func (m *treeModel) move(delta int) {
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
	m.clamp()
}

func (m *treeModel) clamp() {
	if len(m.rows) == 0 {
		m.sel, m.offset = 0, 0
		return
	}
	m.sel = min(max(m.sel, 0), len(m.rows)-1)
	for m.sel > 0 && m.rows[m.sel].note != "" {
		m.sel--
	}
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

func (m *treeModel) matches(sp store.Span) bool {
	if m.query == "" {
		return false
	}
	q := strings.ToLower(m.query)
	return strings.Contains(strings.ToLower(sp.Name), q) ||
		strings.Contains(strings.ToLower(sp.Attrs.ToolName), q)
}

func (m *treeModel) jump(dir int) {
	if m.query == "" || len(m.rows) == 0 {
		return
	}
	for i := 1; i <= len(m.rows); i++ {
		idx := (m.sel + dir*i%len(m.rows) + len(m.rows)) % len(m.rows)
		if m.rows[idx].note == "" && m.matches(m.rows[idx].span) {
			m.sel = idx
			m.clamp()
			return
		}
	}
}

// viewHeight is the row area minus the search or filter bar when open.
func (m *treeModel) viewHeight() int {
	h := m.height
	if m.searching || m.filtering {
		h--
	}
	return max(h, 0)
}

func (m *treeModel) view() string {
	lines := make([]string, 0, m.height)
	h := m.viewHeight()
	if len(m.rows) == 0 {
		lines = append(lines, m.th.Dim.Render("no spans"))
	}
	for i := m.offset; i < len(m.rows) && len(lines) < h; i++ {
		lines = append(lines, m.renderRow(i))
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	if m.searching {
		lines = append(lines, m.input.View())
	}
	if m.filtering {
		lines = append(lines, m.filterBar())
	}
	return strings.Join(lines, "\n")
}

func (m *treeModel) filterBar() string {
	mark := func(hidden bool, label string) string {
		if hidden {
			return m.th.Dim.Render(label)
		}
		return m.th.Accent.Render(label)
	}
	parts := []string{
		mark(m.hideKinds[store.KindAgent], "[a]gent"),
		mark(m.hideKinds[store.KindLLM], "[l]lm"),
		mark(m.hideKinds[store.KindTool], "[t]ool"),
		mark(m.hideKinds[store.KindRetrieval], "[r]etr"),
		mark(m.hideKinds[store.KindOther], "[o]ther"),
	}
	e := m.th.Dim.Render("[e]rrors")
	if m.errOnly {
		e = m.th.Accent.Render("[e]rrors")
	}
	parts = append(parts, e, m.th.Dim.Render("[x] reset"))
	return truncate(strings.Join(parts, " "), m.width)
}

func (m *treeModel) renderRow(i int) string {
	r := m.rows[i]
	if r.note != "" {
		line := strings.Repeat("  ", r.depth) + "    " + r.note
		return m.th.StatusWarn.Render(truncate(line, m.width))
	}
	expander := " "
	if r.kids {
		expander = "-"
		if m.collapsed[r.span.ID] {
			expander = "+"
		}
	}
	name := r.span.Name
	if k := r.span.Kind; k == store.KindLLM || k == store.KindTool || k == store.KindRetrieval {
		name = string(k) + " " + name
	}
	mark, markStyle := " ", m.th.StatusOK
	switch {
	// x is the tool's own error; a span nothing ever came back from is a
	// different failure and reads as one.
	case hasOrphan(m.findings[r.span.ID]):
		mark, markStyle = "?", m.th.StatusErr
	case r.span.Status == "error":
		mark, markStyle = "x", m.th.StatusErr
	case len(m.findings[r.span.ID]) > 0:
		mark, markStyle = "!", m.th.StatusWarn
	case r.span.EndedAt.IsZero():
		mark, markStyle = ".", m.th.StatusRun
	}
	dur := spanDuration(r.span)
	if r.span.EndedAt.IsZero() && mark == "?" {
		dur = "no end"
	}
	left := strings.Repeat("  ", r.depth) + expander + " " + name + " " + mark
	pad := m.width - len([]rune(left)) - len([]rune(dur)) - 1
	if pad < 1 {
		left = truncate(left, max(0, m.width-len([]rune(dur))-2))
		pad = 1
	}
	plain := truncate(left+strings.Repeat(" ", pad)+dur, m.width)
	style := markStyle
	switch {
	case i == m.sel:
		style = m.th.Selected
	case m.matches(r.span):
		style = m.th.Match
	case r.span.Status != "error" && !r.span.EndedAt.IsZero():
		style = m.th.Text
	}
	return style.Render(plain)
}

func spanDuration(sp store.Span) string {
	if sp.StartedAt.IsZero() || sp.EndedAt.IsZero() {
		return ""
	}
	return humanDur(sp.EndedAt.Sub(sp.StartedAt))
}

// humanDur renders a duration ms..s..m..h..d, so a span or run that ran for
// days (a resumed Claude Code session, say) reads as one rather than as an
// unbounded minute count.
func humanDur(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd%02dh", int(d.Hours())/24, int(d.Hours())%24)
	}
}

func truncate(s string, w int) string {
	r := []rune(s)
	if w <= 0 {
		return ""
	}
	if len(r) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}

func hasOrphan(fs []store.Finding) bool {
	for _, f := range fs {
		if f.Type == "orphaned_span" {
			return true
		}
	}
	return false
}
