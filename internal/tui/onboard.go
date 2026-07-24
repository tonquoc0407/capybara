package tui

import "strings"

// waitingView replaces the span tree while the database is empty. A new install
// otherwise shows three blank panes and no way to guess what to do next, which
// is the whole difference between a tool someone keeps and one they delete.
func (m appModel) waitingView(width int) string {
	var out []string
	step := func(label, value string) {
		out = append(out, m.th.Dim.Render(truncate(label, width)))
		for _, line := range wrap(value, max(4, width-2)) {
			out = append(out, "  "+m.th.Text.Render(line))
		}
		out = append(out, "")
	}
	out = append(out, m.th.Text.Render("no traces yet"), "")
	if m.about.OTLP != "" {
		out = append(out, m.th.Dim.Render("listening on"),
			"  "+m.th.Accent.Render(truncate(m.about.OTLP, width-2)), "")
		step("point an instrumented app at it", "OTEL_EXPORTER_OTLP_ENDPOINT="+m.about.OTLP)
	}
	if m.about.OTLPErr != "" {
		out = append(out, m.th.StatusErr.Render(truncate("a port was refused: "+m.about.OTLPErr, width)))
		step("another tool holds it; move ours with", "capybara -otlp 127.0.0.1:4319")
	}
	if m.about.Watching != "" {
		step("tailing claude sessions in", m.about.Watching)
	}
	step("or read a file you already have", "capybara import trace.jsonl")
	step("recording to", m.dbPath)
	return strings.Join(out, "\n")
}

// wrap breaks a value across lines without dropping any of it: these are
// commands and endpoints, and a truncated one cannot be copied.
func wrap(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	var lines []string
	for len(s) > width {
		lines = append(lines, s[:width])
		s = s[width:]
	}
	return append(lines, s)
}
