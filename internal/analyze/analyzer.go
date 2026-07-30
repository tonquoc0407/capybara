package analyze

import (
	"context"
	"fmt"

	"github.com/tonquoc0407/capybara/internal/store"
)

// Analyzer processes ingested spans incrementally: contract checks and
// pricing per span, improvise/loop/spike checks per run. Restart-safe: the
// analyzed flag plus finding dedupe make every sweep idempotent.
type Analyzer struct {
	st       *store.Store
	prices   pricing
	versions map[string]int64
}

// New returns an analyzer over one store, with the user's pricing overrides.
func New(st *store.Store) (*Analyzer, error) {
	prices, err := loadPricing(DefaultPricingPath())
	if err != nil {
		return nil, fmt.Errorf("analyze: %w", err)
	}
	return &Analyzer{st: st, prices: prices, versions: make(map[string]int64)}, nil
}

// Sweep analyzes all completed spans not yet processed.
func (a *Analyzer) Sweep(ctx context.Context) error {
	spans, err := a.st.UnanalyzedSpans(ctx)
	if err != nil {
		return fmt.Errorf("analyze: %w", err)
	}
	if len(spans) == 0 {
		return nil
	}
	var findings []store.Finding
	costs := make(map[string]float64)
	ids := make([]string, 0, len(spans))
	fresh := make(map[string]bool, len(spans))
	runs := make([]string, 0, 1)
	seenRun := make(map[string]bool)
	for _, sp := range spans {
		ids = append(ids, sp.ID)
		fresh[sp.ID] = true
		if !seenRun[sp.RunID] {
			seenRun[sp.RunID] = true
			runs = append(runs, sp.RunID)
		}
		switch sp.Kind {
		case store.KindTool:
			fs, err := a.checkTool(ctx, sp)
			if err != nil {
				return fmt.Errorf("analyze %s: %w", sp.ID, err)
			}
			findings = append(findings, fs...)
		case store.KindLLM:
			if cost := a.prices.spanCost(sp); cost != nil {
				costs[sp.ID] = *cost
			}
		}
	}
	taintsByRun := make(map[string][]store.Taint, len(runs))
	for _, runID := range runs {
		fs, ts, err := a.checkRun(ctx, runID, findings, fresh)
		if err != nil {
			return fmt.Errorf("analyze run %s: %w", runID, err)
		}
		findings = append(findings, fs...)
		taintsByRun[runID] = ts
	}
	if err := a.st.SetSpanCosts(ctx, costs); err != nil {
		return fmt.Errorf("analyze: %w", err)
	}
	if len(findings) > 0 {
		if err := a.st.WriteBatch(ctx, store.Batch{Source: "analyze", Findings: findings}); err != nil {
			return fmt.Errorf("analyze: %w", err)
		}
	}
	for _, runID := range runs {
		if err := a.st.PutTaints(ctx, runID, taintsByRun[runID]); err != nil {
			return fmt.Errorf("analyze: %w", err)
		}
	}
	if err := a.st.MarkAnalyzed(ctx, ids); err != nil {
		return fmt.Errorf("analyze: %w", err)
	}
	return nil
}

// checkRun evaluates the cross-span detectors: improvise, loops, token spikes,
// and the taint edges that link every finding to the run's final output.
func (a *Analyzer) checkRun(ctx context.Context, runID string,
	sweepFindings []store.Finding, fresh map[string]bool,
) ([]store.Finding, []store.Taint, error) {
	spans, err := a.st.Spans(ctx, runID)
	if err != nil {
		return nil, nil, err
	}
	known, err := a.st.Findings(ctx, runID)
	if err != nil {
		return nil, nil, err
	}
	for _, f := range sweepFindings {
		if f.RunID == runID {
			known = append(known, f)
		}
	}
	rc := newRunContext(spans, known, fresh)
	findings, err := a.improviseRun(ctx, rc)
	if err != nil {
		return nil, nil, err
	}
	injections, err := a.injectionRun(ctx, rc)
	if err != nil {
		return nil, nil, err
	}
	findings = append(findings, injections...)
	unfaithful, err := a.faithfulnessRun(ctx, rc)
	if err != nil {
		return nil, nil, err
	}
	findings = append(findings, unfaithful...)
	calls, err := a.st.ToolCalls(ctx, runID)
	if err != nil {
		return nil, nil, err
	}
	findings = append(findings, loopFindings(runID, calls)...)
	findings = append(findings, spikeFindings(spans)...)
	taints := taintRun(spans, append(known, findings...))
	return findings, taints, nil
}

// Watch sweeps once, then again after every committed write, until ctx ends.
// A sweep aborted by ctx cancellation is clean shutdown, not an error.
func (a *Analyzer) Watch(ctx context.Context) error {
	ch, cancel := a.st.Subscribe()
	defer cancel()
	if err := a.sweepUnlessCancelled(ctx); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case _, ok := <-ch:
			if !ok {
				return nil
			}
			if err := a.sweepUnlessCancelled(ctx); err != nil {
				return err
			}
		}
	}
}

func (a *Analyzer) sweepUnlessCancelled(ctx context.Context) error {
	if err := a.Sweep(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	return nil
}
