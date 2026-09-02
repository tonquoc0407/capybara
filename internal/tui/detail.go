package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/tonquoc0407/capybara/internal/store"
	"github.com/tonquoc0407/capybara/internal/theme"
)

// maxRenderedBody keeps huge recorded payloads from stalling the render loop.
const maxRenderedBody = 64 * 1024

type detailModel struct {
	th       theme.Theme
	vp       viewport.Model
	width    int
	height   int
	span     *store.Span
	contents []store.Content
	findings []store.Finding
	sample   *store.ResourceSample
	diff     *diffDetail
	showRaw  bool
	markdown *glamour.TermRenderer
}

// diffDetail is one aligned step with both sides' content.
type diffDetail struct {
	name         string
	runA, runB   string
	sideA, sideB []store.Content
	hasA, hasB   bool
}

func newDetail(th theme.Theme) detailModel {
	return detailModel{th: th, vp: viewport.New(0, 0)}
}

func (m *detailModel) setSize(w, h int) {
	m.width, m.height = w, h
	m.vp.Width, m.vp.Height = w, h
	m.markdown = nil // word wrap depends on width; rebuild lazily
	m.render()
}

func (m *detailModel) setSpan(sp store.Span, contents []store.Content, findings []store.Finding, sample *store.ResourceSample) {
	if m.span == nil || m.span.ID != sp.ID {
		m.vp.GotoTop()
	}
	m.span = &sp
	m.contents = contents
	m.findings = findings
	m.sample = sample
	m.diff = nil
	m.render()
}

// setDiffStep shows both runs' content for one aligned step.
func (m *detailModel) setDiffStep(d diffDetail) {
	m.vp.GotoTop()
	m.diff = &d
	m.span = nil
	m.render()
}

func (m *detailModel) toggleRaw() {
	m.showRaw = !m.showRaw
	m.render()
}

func (m *detailModel) update(msg tea.KeyMsg) tea.Cmd {
	if msg.String() == "a" {
		m.toggleRaw()
		return nil
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return cmd
}

func (m *detailModel) view() string {
	return m.vp.View()
}

func (m *detailModel) render() {
	if m.width <= 0 {
		return
	}
	if m.diff != nil {
		var b strings.Builder
		m.writeDiff(&b)
		m.vp.SetContent(b.String())
		return
	}
	if m.span == nil {
		m.vp.SetContent(m.th.Dim.Render("no span selected"))
		return
	}
	var b strings.Builder
	m.writeHeader(&b)
	m.writeFindings(&b)
	if m.showRaw {
		m.writeRawAttrs(&b)
	} else {
		m.writeContents(&b)
	}
	m.vp.SetContent(b.String())
}

func (m *detailModel) writeFindings(b *strings.Builder) {
	for _, f := range m.findings {
		style := m.th.StatusWarn
		if f.Severity == "error" {
			style = m.th.StatusErr
		}
		for i, line := range findingLines(f) {
			if i > 0 {
				style = m.th.Dim
			}
			b.WriteString(style.Render(line) + "\n")
		}
	}
	if len(m.findings) > 0 {
		b.WriteString("\n")
	}
}

// resourceLine renders the last process reading taken while the span ran.
// CPU comes in as OTel's fraction of one core, which is a percentage here.
func resourceLine(sm *store.ResourceSample) string {
	if sm == nil {
		return ""
	}
	parts := []string{}
	if sm.CPUUtil != nil {
		parts = append(parts, fmt.Sprintf("cpu %.0f%%", *sm.CPUUtil*100))
	}
	if sm.RSSBytes != nil {
		parts = append(parts, "rss "+humanBytes(*sm.RSSBytes))
	}
	return strings.Join(parts, " ")
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 3; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGT"[exp])
}

func (m *detailModel) writeHeader(b *strings.Builder) {
	sp := m.span
	b.WriteString(m.th.Accent.Render(sp.Name) + "\n")
	info := fmt.Sprintf("%s - %s", sp.Kind, sp.Status)
	if d := spanDuration(*sp); d != "" {
		info += " - " + d
	}
	if sp.TokensIn > 0 || sp.TokensOut > 0 {
		info += fmt.Sprintf(" - tok %d/%d", sp.TokensIn, sp.TokensOut)
	}
	if n := cacheRead(*sp); n > 0 {
		info += fmt.Sprintf(" - cached %d", n)
	}
	if sp.CostUSD != nil {
		info += fmt.Sprintf(" - $%.4f", *sp.CostUSD)
	}
	if r := resourceLine(m.sample); r != "" {
		info += " - " + r
	}
	b.WriteString(m.th.Dim.Render(info) + "\n")
	meta := []string{}
	if sp.Attrs.Model != "" {
		meta = append(meta, sp.Attrs.Model)
	}
	if sp.Attrs.Provider != "" {
		meta = append(meta, sp.Attrs.Provider)
	}
	if sp.Attrs.ToolName != "" {
		meta = append(meta, sp.Attrs.ToolName)
	}
	if sp.Attrs.MCP {
		meta = append(meta, "mcp")
	}
	if len(meta) > 0 {
		b.WriteString(m.th.Dim.Render(strings.Join(meta, " - ")) + "\n")
	}
	b.WriteString("\n")
}

func (m *detailModel) writeRawAttrs(b *strings.Builder) {
	b.WriteString(m.th.PaneTitle.Render("attrs") + "\n")
	raw, err := json.MarshalIndent(m.span.Attrs, "", "  ")
	if err != nil {
		b.WriteString(m.th.StatusErr.Render(err.Error()))
		return
	}
	b.WriteString(string(raw) + "\n")
}

func (m *detailModel) writeContents(b *strings.Builder) {
	if m.analysisSkipped() {
		b.WriteString(m.th.Dim.Render("no tool output recorded; contract analysis skipped") + "\n")
	}
	if len(m.contents) == 0 {
		b.WriteString(m.th.Dim.Render("no content recorded"))
		return
	}
	m.writeContentList(b, m.contents)
}

func (m *detailModel) writeContentList(b *strings.Builder, contents []store.Content) {
	for _, c := range contents {
		b.WriteString(m.th.PaneTitle.Render(c.Role) + "\n")
		body := stripControl(c.Body)
		truncated := false
		if len(body) > maxRenderedBody {
			body, truncated = body[:maxRenderedBody], true
		}
		if c.MediaType == "application/json" {
			b.WriteString(prettyJSON(body))
		} else {
			b.WriteString(m.renderMarkdown(body))
		}
		if truncated {
			b.WriteString(m.th.Dim.Render("(truncated)") + "\n")
		}
		b.WriteString("\n")
	}
}

// minColumnWidth is the narrowest a diff side can get before two columns cost
// more in wrapping than they gain in comparison.
const minColumnWidth = 34

func (m *detailModel) writeDiff(b *strings.Builder) {
	d := m.diff
	b.WriteString(m.th.Accent.Render(d.name) + "\n\n")
	if m.width >= 2*minColumnWidth+1 {
		b.WriteString(m.diffColumns(d))
		return
	}
	m.writeSide(b, d.runA, d.sideA, d.hasA)
	b.WriteString("\n")
	m.writeSide(b, d.runB, d.sideB, d.hasB)
}

func (m *detailModel) diffColumns(d *diffDetail) string {
	col := (m.width - 1) / 2
	defer m.narrowTo(col)()
	var left, right strings.Builder
	m.writeSide(&left, d.runA, d.sideA, d.hasA)
	m.writeSide(&right, d.runB, d.sideB, d.hasB)
	style := lipgloss.NewStyle().Width(col)
	return lipgloss.JoinHorizontal(lipgloss.Top,
		style.Render(left.String()), " ", style.Render(right.String()))
}

// narrowTo makes body rendering wrap at a column, and restores the pane width
// when the caller is done with it.
func (m *detailModel) narrowTo(w int) func() {
	full := m.width
	m.width, m.markdown = w, nil
	return func() { m.width, m.markdown = full, nil }
}

func (m *detailModel) writeSide(b *strings.Builder, run string, contents []store.Content, present bool) {
	b.WriteString(m.th.PaneTitle.Render("run "+shortID(run)) + "\n")
	if !present {
		b.WriteString(m.th.Dim.Render("no matching step") + "\n")
		return
	}
	if len(contents) == 0 {
		b.WriteString(m.th.Dim.Render("no content recorded") + "\n")
		return
	}
	m.writeContentList(b, contents)
}

// analysisSkipped reports a completed tool span without recorded output:
// the contract checks read bodies and never guess.
func (m *detailModel) analysisSkipped() bool {
	if m.span.Kind != store.KindTool || m.span.EndedAt.IsZero() {
		return false
	}
	for _, c := range m.contents {
		if c.Role == "output" {
			return false
		}
	}
	return true
}

func (m *detailModel) renderMarkdown(body string) string {
	if m.markdown == nil {
		r, err := glamour.NewTermRenderer(
			glamour.WithStandardStyle(m.th.GlamourStyle),
			glamour.WithWordWrap(max(10, m.width-2)),
		)
		if err != nil {
			return body + "\n"
		}
		m.markdown = r
	}
	out, err := m.markdown.Render(body)
	if err != nil {
		return body + "\n"
	}
	return strings.TrimLeft(out, "\n")
}

// stripControl removes C0/C1 control bytes other than newline and tab. Content
// bodies come from whatever the traced agent recorded, so an ESC-sequence
// payload (cursor moves, OSC 52 clipboard writes, OSC 8 hyperlink spoofing)
// must not reach the terminal verbatim just because a user inspected a span.
func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			return -1
		default:
			return r
		}
	}, s)
}

func prettyJSON(body string) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(body), "", "  "); err != nil {
		return body + "\n"
	}
	return buf.String() + "\n"
}
