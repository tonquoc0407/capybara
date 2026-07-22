package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"

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

func (m *detailModel) setSpan(sp store.Span, contents []store.Content, findings []store.Finding) {
	if m.span == nil || m.span.ID != sp.ID {
		m.vp.GotoTop()
	}
	m.span = &sp
	m.contents = contents
	m.findings = findings
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
	if sp.CostUSD != nil {
		info += fmt.Sprintf(" - $%.4f", *sp.CostUSD)
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
		body := c.Body
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

func (m *detailModel) writeDiff(b *strings.Builder) {
	d := m.diff
	b.WriteString(m.th.Accent.Render(d.name) + "\n\n")
	m.writeSide(b, d.runA, d.sideA, d.hasA)
	b.WriteString("\n")
	m.writeSide(b, d.runB, d.sideB, d.hasB)
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

func prettyJSON(body string) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(body), "", "  "); err != nil {
		return body + "\n"
	}
	return buf.String() + "\n"
}
