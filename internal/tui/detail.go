package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	reflowwrap "github.com/muesli/reflow/wrap"

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
	run      *store.Run
	contents []store.Content
	logs     []store.TraceLog
	findings []store.Finding
	sample   *store.ResourceSample
	diff     *diffDetail
	showRaw  bool
	showLogs bool
	logIndex int
	bodyPage int
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
	if sp.ID == "" {
		m.span = nil
		m.contents = nil
		m.findings = nil
		m.sample = nil
		m.showLogs = false
		m.vp.GotoTop()
		m.render()
		return
	}
	if m.span == nil || m.span.ID != sp.ID {
		m.vp.GotoTop()
		m.bodyPage = 0
	}
	m.span = &sp
	m.contents = contents
	m.findings = findings
	m.sample = sample
	m.diff = nil
	m.bodyPage = min(m.bodyPage, m.bodyPages()-1)
	m.render()
}

func (m *detailModel) setRun(run store.Run) {
	m.run = &run
	m.render()
}

// setDiffStep shows both runs' content for one aligned step.
func (m *detailModel) setDiffStep(d diffDetail) {
	m.vp.GotoTop()
	m.diff = &d
	m.span = nil
	m.bodyPage = 0
	m.render()
}

func (m *detailModel) toggleRaw() {
	m.showRaw = !m.showRaw
	m.render()
}

func (m *detailModel) setLogs(logs []store.TraceLog) {
	m.logs = logs
	m.showLogs = true
	m.logIndex = 0
	m.bodyPage = 0
	m.vp.GotoTop()
	m.render()
}

// yankText returns the primary text payload of the selected span or diff step.
func (m *detailModel) yankText() string {
	if m.showLogs && m.logIndex < len(m.logs) {
		return m.logs[m.logIndex].Body
	}
	if m.diff != nil {
		var parts []string
		for _, c := range m.diff.sideA {
			parts = append(parts, c.Body)
		}
		for _, c := range m.diff.sideB {
			parts = append(parts, c.Body)
		}
		return strings.Join(parts, "\n")
	}
	if len(m.contents) == 0 {
		if m.span != nil {
			return m.span.Name
		}
		return ""
	}
	var outputs []string
	for _, c := range m.contents {
		if c.Role == "output" || c.Role == "assistant" {
			outputs = append(outputs, c.Body)
		}
	}
	if len(outputs) > 0 {
		return strings.Join(outputs, "\n")
	}
	var all []string
	for _, c := range m.contents {
		if c.Body != "" {
			all = append(all, c.Body)
		}
	}
	return strings.Join(all, "\n")
}

func (m *detailModel) update(msg tea.KeyMsg) tea.Cmd {
	if m.showLogs {
		switch msg.String() {
		case "n":
			if m.logIndex+1 < len(m.logs) {
				m.logIndex++
				m.bodyPage = 0
				m.vp.GotoTop()
				m.render()
			}
			return nil
		case "p":
			if m.logIndex > 0 {
				m.logIndex--
				m.bodyPage = 0
				m.vp.GotoTop()
				m.render()
			}
			return nil
		}
	}
	if msg.String() == "a" {
		m.toggleRaw()
		return nil
	}
	if msg.String() == "]" && m.bodyPage+1 < m.bodyPages() {
		m.bodyPage++
		m.vp.GotoTop()
		m.render()
		return nil
	}
	if msg.String() == "[" && m.bodyPage > 0 {
		m.bodyPage--
		m.vp.GotoTop()
		m.render()
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
	if m.showLogs {
		var b strings.Builder
		m.writeLog(&b)
		m.vp.SetContent(b.String())
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

func (m *detailModel) writeLog(b *strings.Builder) {
	b.WriteString(m.th.PaneTitle.Render("linked logs  ·  l back  ·  n/p next/previous log") + "\n\n")
	if len(m.logs) == 0 {
		b.WriteString(m.th.Dim.Render("no linked logs for this run"))
		return
	}
	log := m.logs[m.logIndex]
	b.WriteString(m.th.Accent.Render(reflowwrap.String(stripControl(fmt.Sprintf("log %d/%d  ·  %s", m.logIndex+1, len(m.logs), log.Severity)), max(1, m.width))) + "\n")
	b.WriteString(reflowwrap.String(stripControl("span "+log.SpanID+"  ·  "+log.ObservedAt.Format("2006-01-02 15:04:05.000")), max(1, m.width)) + "\n\n")
	body, pages := bodyChunk(logPayload(log), m.bodyPage)
	if pages > 1 {
		b.WriteString(m.th.Dim.Render(fmt.Sprintf("payload page %d/%d  ·  [/] change page", m.bodyPage+1, pages)) + "\n")
	}
	b.WriteString(reflowwrap.String(body, max(1, m.width)) + "\n")
}

func logPayload(log store.TraceLog) string {
	body := stripControl(log.Body)
	if log.AttrsJSON != "" {
		body += "\n\nattributes\n" + prettyJSON(stripControl(log.AttrsJSON))
	}
	return body
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
	if m.run != nil {
		label := m.run.Label
		if label == "" {
			label = m.run.ID
		}
		b.WriteString(m.th.Dim.Render(reflowwrap.String(stripControl("run "+label), max(1, m.width))) + "\n")
	}
	b.WriteString(m.th.Accent.Render(reflowwrap.String(stripControl(sp.Name), max(1, m.width))) + "\n")
	b.WriteString(m.th.Dim.Render(reflowwrap.String(stripControl("span "+sp.ID+"  ·  run "+sp.RunID), max(1, m.width))) + "\n")
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
	b.WriteString(m.th.Dim.Render(reflowwrap.String(stripControl(info), max(1, m.width))) + "\n")
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
		b.WriteString(m.th.Dim.Render(reflowwrap.String(stripControl(strings.Join(meta, " - ")), max(1, m.width))) + "\n")
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
		body, pages := bodyChunk(body, m.bodyPage)
		if pages > 1 {
			b.WriteString(m.th.Dim.Render(fmt.Sprintf("payload page %d/%d  ·  [/] change page", min(m.bodyPage+1, pages), pages)) + "\n")
		}
		if c.MediaType == "application/json" {
			b.WriteString(reflowwrap.String(prettyJSON(body), max(1, m.width)))
		} else {
			b.WriteString(m.renderMarkdown(body))
		}
		b.WriteString("\n")
	}
}

// bodyChunk bounds rendering while making every byte reachable. Page ends
// advance to the next UTF-8 boundary so changing pages never loses a rune.
func bodyChunk(body string, page int) (string, int) {
	if len(body) <= maxRenderedBody {
		return body, 1
	}
	starts := []int{0}
	for start := 0; start < len(body); {
		end := min(start+maxRenderedBody, len(body))
		for end < len(body) && !utf8.RuneStart(body[end]) {
			end++
		}
		if end == len(body) {
			break
		}
		starts = append(starts, end)
		start = end
	}
	if page >= len(starts) {
		return "", len(starts)
	}
	end := len(body)
	if page+1 < len(starts) {
		end = starts[page+1]
	}
	return body[starts[page]:end], len(starts)
}

func (m *detailModel) bodyPages() int {
	if m.showLogs {
		if m.logIndex >= len(m.logs) {
			return 1
		}
		_, n := bodyChunk(logPayload(m.logs[m.logIndex]), 0)
		return n
	}
	pages := 1
	check := func(contents []store.Content) {
		for _, c := range contents {
			_, n := bodyChunk(stripControl(c.Body), 0)
			pages = max(pages, n)
		}
	}
	if m.diff != nil {
		check(m.diff.sideA)
		check(m.diff.sideB)
	} else {
		check(m.contents)
	}
	return pages
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
