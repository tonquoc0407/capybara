package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

// labelWidth is the summary's left column: the longest label plus a space.
const labelWidth = 8

// summaryView answers what a run was, which the id alone never does: when it
// ran, what drove it, what it spent, and what was recorded against it.
func (m appModel) summaryView(width int) string {
	run, ok := m.runs.selectedRun()
	if !ok {
		return m.th.Dim.Render("no run selected")
	}
	rows := [][2]string{
		{"started", stamp(run.StartedAt)},
		{"source", run.Source},
		{"model", shortModel(run.ModelMain)},
		{"spans", spanCounts(m.spans)},
	}
	if run.TokensIn > 0 || run.TokensOut > 0 {
		rows = append(rows, [2]string{"tokens", fmt.Sprintf("%s in %s out",
			compactCount(run.TokensIn), compactCount(run.TokensOut))})
	}
	if run.CostUSD != nil {
		rows = append(rows, [2]string{"cost", fmt.Sprintf("$%.4f", *run.CostUSD)})
	}
	lines := make([]string, 0, len(rows)+4)
	for _, r := range rows {
		if r[1] == "" {
			continue
		}
		lines = append(lines, m.th.Dim.Render(pad(r[0], labelWidth))+
			m.th.Text.Render(truncate(r[1], max(1, width-labelWidth))))
	}
	return strings.Join(append(lines, m.findingLines(width)...), "\n")
}

// findingLines break the count down by type, since "3 findings" does not say
// whether the run drifted or invented an answer.
func (m appModel) findingLines(width int) []string {
	byType := map[string]int{}
	worst := map[string]string{}
	count := func(f store.Finding) {
		byType[f.Type]++
		if f.Severity == "error" {
			worst[f.Type] = "error"
		}
	}
	for _, fs := range m.findings {
		for _, f := range fs {
			count(f)
		}
	}
	// Run-level findings hang off no span, so the per-span map never sees them.
	for _, f := range m.runFindings {
		count(f)
	}
	if len(byType) == 0 {
		return []string{"", m.th.StatusOK.Render("no findings")}
	}
	types := make([]string, 0, len(byType))
	for t := range byType {
		types = append(types, t)
	}
	sort.Strings(types)
	out := []string{"", m.th.Dim.Render("findings")}
	for _, t := range types {
		style := m.th.StatusWarn
		if worst[t] == "error" {
			style = m.th.StatusErr
		}
		out = append(out, style.Render(truncate(fmt.Sprintf("%d %s", byType[t], t), max(1, width))))
	}
	return out
}

// stamp drops the year, and the date too when the run is from today, because
// the pane is narrow and the common case is a run from minutes ago.
func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	t = t.Local()
	if y, m, d := t.Date(); y == time.Now().Year() {
		if ny, nm, nd := time.Now().Date(); y == ny && m == nm && d == nd {
			return t.Format("15:04:05")
		}
		return t.Format("Jan 2 15:04")
	}
	return t.Format("2006-01-02 15:04")
}

func spanCounts(spans []store.Span) string {
	if len(spans) == 0 {
		return ""
	}
	byKind := map[store.Kind]int{}
	for _, sp := range spans {
		byKind[sp.Kind]++
	}
	parts := []string{fmt.Sprintf("%d", len(spans))}
	for _, k := range []store.Kind{store.KindLLM, store.KindTool} {
		if n := byKind[k]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, k))
		}
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return parts[0] + " (" + strings.Join(parts[1:], ", ") + ")"
}

func pad(s string, w int) string {
	if len(s) >= w {
		return s + " "
	}
	return s + strings.Repeat(" ", w-len(s))
}

func compactCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 10_000:
		return fmt.Sprintf("%dk", n/1000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	default:
		return fmt.Sprintf("%d", n)
	}
}
