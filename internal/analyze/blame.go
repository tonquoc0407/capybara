package analyze

import (
	"context"
	"sort"

	"github.com/tonquoc0407/capybara/internal/store"
)

// BlameHop is one span on the path from a run's final output back toward the
// source of a finding. Root marks a span nothing else tainted, the thing to
// fix. Depth is how far the hop sits from the final output, so a span tainted
// by several sources renders as branches under it.
type BlameHop struct {
	Span     store.Span
	Findings []store.Finding
	Root     bool
	Depth    int
}

// BlameChain is the tainted path from a run's final output to its sources,
// flattened depth-first, earliest source first. Hops is empty when the final
// output carries no taint.
type BlameChain struct {
	RunID string
	Hops  []BlameHop
}

// Blame walks a run's final output back to the earliest span that tainted it.
func Blame(ctx context.Context, st *store.Store, runID string) (*BlameChain, error) {
	spans, err := st.Spans(ctx, runID)
	if err != nil {
		return nil, err
	}
	findings, err := st.Findings(ctx, runID)
	if err != nil {
		return nil, err
	}
	taints, err := st.Taints(ctx, runID)
	if err != nil {
		return nil, err
	}
	return blame(runID, spans, findings, taints), nil
}

func blame(runID string, spans []store.Span, findings []store.Finding, taints []store.Taint) *BlameChain {
	chain := &BlameChain{RunID: runID}
	start := finalOutputSpan(spans)
	if start == nil {
		return chain
	}
	byID := make(map[string]store.Span, len(spans))
	for _, sp := range spans {
		byID[sp.ID] = sp
	}
	bySpan := make(map[string][]store.Finding)
	for _, f := range findings {
		if f.SpanID != "" {
			bySpan[f.SpanID] = append(bySpan[f.SpanID], f)
		}
	}
	sources := make(map[string][]string)
	tainted := make(map[string]bool)
	for _, t := range taints {
		sources[t.SpanID] = append(sources[t.SpanID], t.SourceSpanID)
		tainted[t.SpanID] = true
	}
	if !tainted[start.ID] && len(bySpan[start.ID]) == 0 {
		return chain
	}
	visited := map[string]bool{start.ID: true}
	var walk func(sp store.Span, depth int)
	walk = func(sp store.Span, depth int) {
		next := unvisitedSources(sources[sp.ID], visited, byID)
		chain.Hops = append(chain.Hops, BlameHop{
			Span:     sp,
			Findings: bySpan[sp.ID],
			Root:     len(sources[sp.ID]) == 0,
			Depth:    depth,
		})
		for _, src := range next {
			visited[src.ID] = true
		}
		for _, src := range next {
			walk(src, depth+1)
		}
	}
	walk(*start, 0)
	return chain
}

// unvisitedSources are the spans that tainted this one, earliest first so the
// first branch walked is the one closest to the origin.
func unvisitedSources(ids []string, visited map[string]bool, byID map[string]store.Span) []store.Span {
	var candidates []store.Span
	for _, id := range ids {
		if visited[id] {
			continue
		}
		if sp, ok := byID[id]; ok {
			candidates = append(candidates, sp)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if !candidates[i].StartedAt.Equal(candidates[j].StartedAt) {
			return candidates[i].StartedAt.Before(candidates[j].StartedAt)
		}
		return candidates[i].ID < candidates[j].ID
	})
	return candidates
}

func finalOutputSpan(spans []store.Span) *store.Span {
	var best *store.Span
	pick := func(i int) {
		if best == nil || laterEnd(spans[i], *best) {
			best = &spans[i]
		}
	}
	for i := range spans {
		if spans[i].ParentID == "" {
			pick(i)
		}
	}
	if best != nil {
		return best
	}
	for i := range spans {
		pick(i)
	}
	return best
}

func laterEnd(a, b store.Span) bool {
	if !a.EndedAt.Equal(b.EndedAt) {
		return a.EndedAt.After(b.EndedAt)
	}
	return a.ID > b.ID
}
