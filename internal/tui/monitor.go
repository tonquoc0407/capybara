package tui

import (
	"fmt"
	"strings"

	"github.com/tonquoc0407/capybara/internal/store"
	"github.com/tonquoc0407/capybara/internal/theme"
)

// The monitor is the one view that reads samples instead of spans, so it has
// something to show while a run is still going: a span only arrives once it
// ends, but a reading arrives every second either way.
type monitorModel struct {
	th      theme.Theme
	width   int
	height  int
	samples []store.ResourceSample
}

func newMonitor(th theme.Theme) monitorModel {
	return monitorModel{th: th}
}

func (m *monitorModel) setSize(w, h int) { m.width, m.height = w, h }

func (m *monitorModel) setSamples(samples []store.ResourceSample) { m.samples = samples }

// series is one metric's history plus how to label it.
type series struct {
	title  string
	values []float64
	// scale is the graph's ceiling. A fraction of a core is pinned to 1 so two
	// runs read the same; bytes grow to fit, since there is no honest ceiling.
	scale  float64
	format func(float64) string
	last   float64
	have   bool
}

func (m monitorModel) view() string {
	if len(m.samples) == 0 {
		return m.th.Dim.Render("no readings yet\n\nresource sampling is on for a local\ncapybara when the run uses the sdk")
	}
	all := m.buildSeries()
	shown := make([]series, 0, len(all))
	for _, s := range all {
		if s.have {
			shown = append(shown, s)
		}
	}
	if len(shown) == 0 {
		return m.th.Dim.Render("no readings yet")
	}
	// Two rows of chrome per graph: the title line and a blank separator.
	graphHeight := max(2, (m.height-len(shown)*2)/max(1, len(shown)))
	var b strings.Builder
	for i, s := range shown {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(m.renderSeries(s, graphHeight))
	}
	b.WriteString(m.timeline())
	return b.String()
}

func (m monitorModel) renderSeries(s series, height int) string {
	head := fmt.Sprintf("%-10s %s", s.title, s.format(s.last))
	rows := brailleGraph(s.values, s.scale, max(1, m.width), height)
	var b strings.Builder
	b.WriteString(m.th.Accent.Render(head) + "\n")
	for _, r := range rows {
		b.WriteString(m.th.Text.Render(r) + "\n")
	}
	return b.String()
}

// timeline names the node each stretch of the graph was taken under, so a spike
// points at a step rather than at a moment.
func (m monitorModel) timeline() string {
	var names []string
	var seen string
	for _, sm := range m.samples {
		name := nodeLabel(sm)
		if name != seen {
			names = append(names, name)
			seen = name
		}
	}
	if len(names) == 0 {
		return ""
	}
	line := strings.Join(names, " > ")
	return m.th.Dim.Render(truncate("nodes: "+line, m.width))
}

func (m monitorModel) buildSeries() []series {
	cpu := series{title: "cpu", scale: 1, format: percent}
	rss := series{title: "memory", format: bytesOf}
	gpu := series{title: "gpu", scale: 1, format: percent}
	gpuMem := series{title: "gpu mem", format: bytesOf}
	for _, sm := range m.samples {
		addFloat(&cpu, sm.CPUUtil)
		addInt(&rss, sm.RSSBytes)
		addFloat(&gpu, sm.GPUUtil)
		addInt(&gpuMem, sm.GPUMemBytes)
	}
	// Utilization can exceed one core; let the graph grow rather than clip.
	cpu.scale = max(cpu.scale, cpu.peak())
	gpu.scale = max(gpu.scale, gpu.peak())
	rss.scale = rss.peak()
	gpuMem.scale = gpuMem.peak()
	return []series{cpu, rss, gpu, gpuMem}
}

func (s series) peak() float64 {
	var top float64
	for _, v := range s.values {
		top = max(top, v)
	}
	return top
}

// A gauge missing from one reading holds its last value rather than dropping to
// zero: the metric was not zero, it was not read that tick.
func addFloat(s *series, v *float64) {
	if v == nil {
		s.values = append(s.values, s.last)
		return
	}
	s.values = append(s.values, *v)
	s.last, s.have = *v, true
}

func addInt(s *series, v *int64) {
	if v == nil {
		s.values = append(s.values, s.last)
		return
	}
	f := float64(*v)
	s.values = append(s.values, f)
	s.last, s.have = f, true
}

func percent(v float64) string { return fmt.Sprintf("%.0f%%", v*100) }

func bytesOf(v float64) string { return humanBytes(int64(v)) }

// nodeLabel is the timeline's name for a reading. A span name arrives as the
// operation plus the tool ("execute_tool embed_corpus"); the operation is the
// same for every node in the row and only eats the width, so it goes.
func nodeLabel(sm store.ResourceSample) string {
	if sm.SpanName == "" {
		return shortSpan(sm.SpanID)
	}
	for _, op := range []string{"execute_tool ", "invoke_agent ", "chat "} {
		if rest, ok := strings.CutPrefix(sm.SpanName, op); ok && rest != "" {
			return rest
		}
	}
	return sm.SpanName
}

func shortSpan(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
