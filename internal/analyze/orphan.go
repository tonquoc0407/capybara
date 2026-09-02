package analyze

import (
	"context"
	"encoding/json"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

// A span is exported only once it ends, so a process killed mid-call leaves no
// span at all - the last resource sample taken under it is the only record that
// the node ever ran. Sampling stops when the process stops, so a gap is the
// liveness signal. The gap has to clear three missed exports plus slack, or a
// stop-the-world pause reads as a death.
const orphanGap = 5 * time.Second

// orphanRun flags where a sampled run stopped responding. Readings are
// attributed to the innermost active span, so among the spans still open the
// one sampled most recently is where execution was when it stopped; the rest
// are its ancestors, waiting on it. One process dies once, so one finding.
func (a *Analyzer) orphanRun(ctx context.Context, runID string, now time.Time) (*store.Finding, error) {
	sampled, err := a.st.SampledSpans(ctx, runID)
	if err != nil {
		return nil, err
	}
	var last *store.SampledSpan
	open := 0
	for i, sp := range sampled {
		if sp.Ended {
			continue
		}
		open++
		if last == nil || sp.LastSample.After(last.LastSample) {
			last = &sampled[i]
		}
	}
	if last == nil || now.Sub(last.LastSample) < orphanGap {
		return nil, nil
	}
	// Only what was observed, never how long ago it was: the elapsed time grows
	// with every tick, and the detail is part of the finding's identity, so a
	// moving one would file the same death again on every sweep.
	stamp := last.LastSample.UTC().Format(time.RFC3339)
	detail := map[string]any{
		"evidence":    "no resource sample since " + stamp + ", span never closed",
		"last_sample": stamp,
	}
	if last.Name != "" {
		detail["span_name"] = last.Name
	}
	if open > 1 {
		detail["open_ancestors"] = open - 1
	}
	// Anchoring on a span the store never received would dangle; the id still
	// belongs in the detail, since it is all the samples left to identify it by.
	spanID := last.SpanID
	if !last.Known {
		detail["unreported_span"] = last.SpanID
		spanID = ""
	}
	raw, _ := json.Marshal(detail)
	return &store.Finding{
		RunID: runID, SpanID: spanID, Type: "orphaned_span",
		Severity: "error", Detail: string(raw),
	}, nil
}

func (a *Analyzer) orphanPass(ctx context.Context, now time.Time) ([]store.Finding, error) {
	runs, err := a.st.RunsWithOpenSamples(ctx)
	if err != nil {
		return nil, err
	}
	var findings []store.Finding
	for _, runID := range runs {
		f, err := a.orphanRun(ctx, runID, now)
		if err != nil {
			return nil, err
		}
		if f != nil {
			findings = append(findings, *f)
		}
	}
	return findings, nil
}
