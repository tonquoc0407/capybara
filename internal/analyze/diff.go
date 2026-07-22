package analyze

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

// DiffStep is one aligned position of a run comparison. A or B is nil where
// the other run has a step with no counterpart.
type DiffStep struct {
	A, B     *store.Span
	Diverged bool
}

// RunDiff compares two runs step by step, aligned by position and tool name.
type RunDiff struct {
	RunA, RunB      string
	Steps           []DiffStep
	FirstDivergence int // index into Steps; -1 when the runs agree
	ContentsA       map[string][]store.Content
	ContentsB       map[string][]store.Content
}

// DTokens is the token delta of one step, B minus A.
func (s DiffStep) DTokens() int64 {
	return spanTokens(s.B) - spanTokens(s.A)
}

// DCost is the cost delta of one step, or nil when neither side is priced.
func (s DiffStep) DCost() *float64 {
	a, b := spanCostValue(s.A), spanCostValue(s.B)
	if a == nil && b == nil {
		return nil
	}
	d := value(b) - value(a)
	return &d
}

// DLatency is the duration delta of one step, B minus A.
func (s DiffStep) DLatency() time.Duration {
	return spanLatency(s.B) - spanLatency(s.A)
}

// DiffRuns aligns two runs and marks where they diverge.
func DiffRuns(ctx context.Context, st *store.Store, runA, runB string) (*RunDiff, error) {
	stepsA, contentsA, err := runSteps(ctx, st, runA)
	if err != nil {
		return nil, err
	}
	stepsB, contentsB, err := runSteps(ctx, st, runB)
	if err != nil {
		return nil, err
	}
	d := &RunDiff{
		RunA: runA, RunB: runB,
		FirstDivergence: -1,
		ContentsA:       contentsA, ContentsB: contentsB,
	}
	for _, p := range align(stepsA, stepsB) {
		step := DiffStep{}
		if p.ai >= 0 {
			step.A = &stepsA[p.ai]
		}
		if p.bi >= 0 {
			step.B = &stepsB[p.bi]
		}
		step.Diverged = diverged(step, contentsA, contentsB)
		if step.Diverged && d.FirstDivergence < 0 {
			d.FirstDivergence = len(d.Steps)
		}
		d.Steps = append(d.Steps, step)
	}
	return d, nil
}

// runSteps returns a run's comparable spans in order: everything but roots.
func runSteps(ctx context.Context, st *store.Store, runID string) ([]store.Span, map[string][]store.Content, error) {
	spans, err := st.Spans(ctx, runID)
	if err != nil {
		return nil, nil, err
	}
	contents, err := st.ContentsForRun(ctx, runID)
	if err != nil {
		return nil, nil, err
	}
	steps := spans[:0]
	for _, sp := range spans {
		// Parentless agent spans are session wrappers, not steps; flat
		// imports legitimately have parentless llm and tool spans.
		if sp.ParentID == "" && sp.Kind == store.KindAgent {
			continue
		}
		steps = append(steps, sp)
	}
	sort.SliceStable(steps, func(i, j int) bool {
		if !steps[i].StartedAt.Equal(steps[j].StartedAt) {
			return steps[i].StartedAt.Before(steps[j].StartedAt)
		}
		return steps[i].ID < steps[j].ID
	})
	return steps, contents, nil
}

// alignKey makes two steps alignable: same kind, and for tools the same tool.
func alignKey(sp store.Span) string {
	if sp.Kind == store.KindTool {
		return "tool\x00" + toolName(sp)
	}
	return string(sp.Kind)
}

type alignedPair struct {
	ai, bi int // -1 marks a gap
}

// align is a longest-common-subsequence walk over the two key sequences.
func align(a, b []store.Span) []alignedPair {
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if alignKey(a[i]) == alignKey(b[j]) {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var pairs []alignedPair
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case alignKey(a[i]) == alignKey(b[j]):
			pairs = append(pairs, alignedPair{ai: i, bi: j})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			pairs = append(pairs, alignedPair{ai: i, bi: -1})
			i++
		default:
			pairs = append(pairs, alignedPair{ai: -1, bi: j})
			j++
		}
	}
	for ; i < n; i++ {
		pairs = append(pairs, alignedPair{ai: i, bi: -1})
	}
	for ; j < m; j++ {
		pairs = append(pairs, alignedPair{ai: -1, bi: j})
	}
	return pairs
}

// diverged marks a step whose two sides differ in presence, status or content.
func diverged(s DiffStep, contentsA, contentsB map[string][]store.Content) bool {
	if s.A == nil || s.B == nil {
		return true
	}
	if s.A.Status != s.B.Status {
		return true
	}
	ca, cb := contentsA[s.A.ID], contentsB[s.B.ID]
	if len(ca) != len(cb) {
		return true
	}
	for i := range ca {
		if ca[i].Role != cb[i].Role || ca[i].Body != cb[i].Body {
			return true
		}
	}
	return false
}

// StepName labels an aligned step by whichever side exists.
func (s DiffStep) StepName() string {
	sp := s.A
	if sp == nil {
		sp = s.B
	}
	if sp.Kind == store.KindTool {
		return "tool " + toolName(*sp)
	}
	if sp.Kind == store.KindLLM {
		return sp.Name
	}
	return fmt.Sprintf("%s %s", sp.Kind, sp.Name)
}

func spanTokens(sp *store.Span) int64 {
	if sp == nil {
		return 0
	}
	return sp.TokensIn + sp.TokensOut
}

func spanCostValue(sp *store.Span) *float64 {
	if sp == nil {
		return nil
	}
	return sp.CostUSD
}

func spanLatency(sp *store.Span) time.Duration {
	if sp == nil || sp.StartedAt.IsZero() || sp.EndedAt.IsZero() {
		return 0
	}
	return sp.EndedAt.Sub(sp.StartedAt)
}

func value(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}
