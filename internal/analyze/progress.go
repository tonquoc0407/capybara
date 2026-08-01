package analyze

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/tonquoc0407/capybara/internal/store"
)

// No-progress detection flags a run whose model keeps producing the same answer
// turn after turn: it is stuck, not converging. This is the multi-turn cousin of
// the loop check, which reads tool calls; here the repetition is in the model's
// own output, so a run with no tools at all is still caught. A short line like
// "working on it" repeats harmlessly, so only a substantial answer counts, and
// it must recur across at least three turns before the run is called stuck.
const (
	noProgressRepeats = 3
	noProgressMinLen  = 40
)

func (a *Analyzer) noProgressRun(ctx context.Context, rc *runContext) ([]store.Finding, error) {
	counts := make(map[string]int)
	last := make(map[string]store.Span)
	for _, sp := range rc.llms {
		text, err := a.assistantText(ctx, sp)
		if err != nil {
			return nil, err
		}
		norm := strings.Join(strings.Fields(text), " ")
		if len(norm) < noProgressMinLen {
			continue
		}
		counts[norm]++
		last[norm] = sp
	}
	var findings []store.Finding
	for norm, n := range counts {
		if n < noProgressRepeats || !rc.fresh[last[norm].ID] {
			continue
		}
		findings = append(findings, finding(last[norm], "no_progress", "warning", map[string]any{
			"evidence": fmt.Sprintf("same answer on %d turns", n),
		}))
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].SpanID < findings[j].SpanID })
	return findings, nil
}

func (a *Analyzer) assistantText(ctx context.Context, sp store.Span) (string, error) {
	cs, err := a.st.Contents(ctx, sp.ID)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, c := range cs {
		if c.Role == "assistant" {
			b.WriteString(c.Body)
			b.WriteByte('\n')
		}
	}
	return b.String(), nil
}
