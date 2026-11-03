package analyze

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

// Analyzer processes ingested spans incrementally: contract checks and
// pricing per span, improvise/loop/spike checks per run. Restart-safe: the
// analyzed flag plus finding dedupe make every sweep idempotent.
type Analyzer struct {
	st           *store.Store
	prices       pricing
	priceSource  string
	priceVersion string
	versions     map[schemaScope]int64
}

type schemaScope struct{ Project, Tool string }

// New returns an analyzer over one store, with the user's pricing overrides.
func New(st *store.Store) (*Analyzer, error) {
	pricePath := DefaultPricingPath()
	prices, err := loadPricing(pricePath)
	if err != nil {
		return nil, fmt.Errorf("analyze: %w", err)
	}
	raw, err := json.Marshal(prices)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(raw)
	source := "capybara embedded pricing"
	if _, err := os.Stat(pricePath); err == nil {
		source = "capybara embedded + local override"
	}
	return &Analyzer{
		st: st, prices: prices, priceSource: source,
		priceVersion: hex.EncodeToString(hash[:8]), versions: make(map[schemaScope]int64),
	}, nil
}

// Sweep analyzes all completed spans not yet processed.
func (a *Analyzer) Sweep(ctx context.Context) error {
	// Runs first: a process that died stops writing, so waiting for a fresh
	// span would mean the crash it left behind is never looked at.
	orphans, err := a.orphanPass(ctx, time.Now())
	if err != nil {
		return fmt.Errorf("analyze: %w", err)
	}
	changedOrphans, err := a.st.ReconcileOrphans(ctx, orphans)
	if err != nil {
		return fmt.Errorf("analyze reconcile orphans: %w", err)
	}
	for _, runID := range changedOrphans {
		spans, err := a.st.Spans(ctx, runID)
		if err != nil {
			return fmt.Errorf("analyze: %w", err)
		}
		findings, err := a.st.Findings(ctx, runID)
		if err != nil {
			return fmt.Errorf("analyze: %w", err)
		}
		if err := a.st.PutTaints(ctx, runID, taintRun(spans, findings)); err != nil {
			return fmt.Errorf("analyze: %w", err)
		}
	}
	spans, err := a.st.UnanalyzedSpans(ctx)
	if err != nil {
		return fmt.Errorf("analyze: %w", err)
	}
	if len(spans) == 0 {
		return nil
	}
	var spanFindings []store.Finding
	costs := make(map[string]store.SpanEstimate)
	fresh := make(map[string]bool, len(spans))
	freshByRun := make(map[string][]store.Span)
	spanFindingsByRun := make(map[string][]store.Finding)
	runs := make([]string, 0, 1)
	seenRun := make(map[string]bool)
	for _, sp := range spans {
		fresh[sp.ID] = true
		freshByRun[sp.RunID] = append(freshByRun[sp.RunID], sp)
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
			for i := range fs {
				fs[i].Origin = "analyzer_span"
			}
			spanFindings = append(spanFindings, fs...)
			spanFindingsByRun[sp.RunID] = append(spanFindingsByRun[sp.RunID], fs...)
		case store.KindLLM:
			if cost := a.prices.spanCost(sp); cost != nil {
				usageSource := "observed token totals"
				if _, ok := sp.Attrs.Raw["usage"]; ok {
					usageSource = "observed usage breakdown"
				}
				costs[sp.ID] = store.SpanEstimate{
					CostUSD: *cost, Revision: sp.Revision,
					Provenance: store.CostProvenance{
						Kind: "estimate", PriceSource: a.priceSource,
						PriceVersion: a.priceVersion, CheckedAt: time.Now().UTC(), UsageSource: usageSource,
					},
				}
			}
		}
		if f := a.checkRateLimit(ctx, sp); f != nil {
			f.Origin = "analyzer_span"
			spanFindings = append(spanFindings, *f)
			spanFindingsByRun[sp.RunID] = append(spanFindingsByRun[sp.RunID], *f)
		}
	}
	if err := a.st.SetAutomaticSpanEstimates(ctx, costs); err != nil {
		return fmt.Errorf("analyze price spans: %w", err)
	}
	var acknowledged []store.Span
	for _, runID := range runs {
		fs, ts, snapshot, err := a.checkRun(ctx, runID, spanFindings, fresh)
		if err != nil {
			return fmt.Errorf("analyze run %s: %w", runID, err)
		}
		for i := range fs {
			fs[i].Origin = "analyzer_run"
		}
		findings := append(spanFindingsByRun[runID], fs...)
		if err := a.st.ReplaceAnalysis(ctx, runID, snapshot, freshByRun[runID], findings, ts); err != nil {
			if errors.Is(err, store.ErrAnalysisStale) {
				continue
			}
			return fmt.Errorf("analyze replace run %s: %w", runID, err)
		}
		acknowledged = append(acknowledged, freshByRun[runID]...)
	}
	if err := a.st.MarkAnalyzed(ctx, acknowledged); err != nil {
		return fmt.Errorf("analyze: %w", err)
	}
	return nil
}

// checkRun evaluates the cross-span detectors: improvise, loops, token spikes,
// and the taint edges that link every finding to the run's final output.
func (a *Analyzer) checkRun(ctx context.Context, runID string,
	sweepFindings []store.Finding, fresh map[string]bool,
) ([]store.Finding, []store.Taint, []store.Span, error) {
	spans, err := a.st.Spans(ctx, runID)
	if err != nil {
		return nil, nil, nil, err
	}
	known, err := a.st.Findings(ctx, runID)
	if err != nil {
		return nil, nil, nil, err
	}
	recomputeAll := false
	for _, sp := range spans {
		if fresh[sp.ID] && sp.Revision > 1 {
			recomputeAll = true
			break
		}
	}
	filtered := known[:0]
	for _, f := range known {
		if f.Origin == "analyzer_run" {
			recomputeAll = true
			continue
		}
		if f.Origin == "analyzer_span" && fresh[f.SpanID] {
			continue
		}
		filtered = append(filtered, f)
	}
	known = filtered
	for _, f := range sweepFindings {
		if f.RunID == runID {
			known = append(known, f)
		}
	}
	active := fresh
	if recomputeAll {
		active = make(map[string]bool, len(spans))
		for _, sp := range spans {
			active[sp.ID] = true
		}
	}
	rc := newRunContext(spans, known, active)
	findings, err := a.improviseRun(ctx, rc)
	if err != nil {
		return nil, nil, nil, err
	}
	injections, err := a.injectionRun(ctx, rc)
	if err != nil {
		return nil, nil, nil, err
	}
	findings = append(findings, injections...)
	unfaithful, err := a.faithfulnessRun(ctx, rc)
	if err != nil {
		return nil, nil, nil, err
	}
	findings = append(findings, unfaithful...)
	secrets, err := a.secretRun(ctx, rc)
	if err != nil {
		return nil, nil, nil, err
	}
	findings = append(findings, secrets...)
	stuck, err := a.noProgressRun(ctx, rc)
	if err != nil {
		return nil, nil, nil, err
	}
	findings = append(findings, stuck...)
	findings = append(findings, a.truncationRun(ctx, rc)...)
	calls, err := a.st.ToolCalls(ctx, runID)
	if err != nil {
		return nil, nil, nil, err
	}
	findings = append(findings, loopFindings(runID, calls)...)
	errorSpans := make(map[string]bool)
	for _, f := range append(known, findings...) {
		switch f.Type {
		case "tool_error", "malformed", "empty_payload", "rate_limited", "malformed_arguments":
			if f.SpanID != "" {
				errorSpans[f.SpanID] = true
			}
		}
	}
	findings = append(findings, oscillationFindings(runID, calls, errorSpans)...)
	findings = append(findings, spikeFindings(spans)...)
	taints := taintRun(spans, append(known, findings...))
	return findings, taints, spans, nil
}

// Watch sweeps once, then again after every committed write, until ctx ends.
// A sweep aborted by ctx cancellation is clean shutdown, not an error.
func (a *Analyzer) Watch(ctx context.Context) error {
	ch, cancel := a.st.Subscribe()
	defer cancel()
	if err := a.sweepUnlessCancelled(ctx); err != nil {
		return err
	}
	// A crashed run writes nothing more, so the write signal alone would never
	// bring the orphan check back around. The tick only leads to a sweep while
	// some run is actually sampling, which costs one indexed lookup otherwise.
	tick := time.NewTicker(orphanGap)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			open, err := a.st.RunsWithOpenSamples(ctx)
			if err != nil || len(open) == 0 {
				continue
			}
			if err := a.sweepUnlessCancelled(ctx); err != nil {
				return err
			}
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
