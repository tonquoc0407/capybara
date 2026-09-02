package analyze

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

func sampled(st *store.Store, t *testing.T, spanID string, at time.Time) {
	t.Helper()
	cpu, rss := 0.5, int64(1024)
	if err := st.PutResourceSamples(context.Background(), "test", []store.ResourceSample{
		{RunID: "r1", SpanID: spanID, At: at, CPUUtil: &cpu, RSSBytes: &rss},
	}); err != nil {
		t.Fatalf("PutResourceSamples: %v", err)
	}
}

func analyzerOn(t *testing.T, st *store.Store) *Analyzer {
	t.Helper()
	a, err := New(st)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

func TestOrphanFlagsASpanThatStoppedReporting(t *testing.T) {
	st := openTemp(t)
	now := time.Now()
	sampled(st, t, "ghost", now.Add(-30*time.Second))
	a := analyzerOn(t, st)
	f, err := a.orphanRun(context.Background(), "r1", now)
	if err != nil {
		t.Fatalf("orphanRun: %v", err)
	}
	if f == nil {
		t.Fatal("no finding for a span sampled 30s ago and never closed")
	}
	if f.Type != "orphaned_span" || f.Severity != "error" || f.SpanID != "" {
		t.Errorf("finding = %+v, want a run-level error orphaned_span", f)
	}
	if !strings.Contains(FindingSummary(*f), "an unreported span") {
		t.Errorf("summary = %q, want it to admit the span never arrived", FindingSummary(*f))
	}
}

// Sampling stops when the process stops, so a run still ticking is alive even
// if the span has been open for a long time.
func TestOrphanIgnoresARunStillReporting(t *testing.T) {
	st := openTemp(t)
	now := time.Now()
	sampled(st, t, "slow", now.Add(-time.Hour))
	sampled(st, t, "slow", now.Add(-time.Second))
	a := analyzerOn(t, st)
	f, err := a.orphanRun(context.Background(), "r1", now)
	if err != nil {
		t.Fatalf("orphanRun: %v", err)
	}
	if f != nil {
		t.Errorf("finding = %+v, want none while samples keep arriving", f)
	}
}

func TestOrphanIgnoresSpansThatClosedNormally(t *testing.T) {
	st := openTemp(t)
	now := time.Now()
	if err := st.WriteBatch(context.Background(), store.Batch{
		Source: "test",
		Spans: []store.Span{{
			ID: "done", RunID: "r1", Kind: store.KindTool, Name: "lookup",
			StartedAt: now.Add(-time.Minute), EndedAt: now.Add(-50 * time.Second), Status: "ok",
		}},
	}); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	sampled(st, t, "done", now.Add(-55*time.Second))
	a := analyzerOn(t, st)
	f, err := a.orphanRun(context.Background(), "r1", now)
	if err != nil {
		t.Fatalf("orphanRun: %v", err)
	}
	if f != nil {
		t.Errorf("finding = %+v, want none for a span that ended", f)
	}
}

// The innermost span is sampled last; its ancestors are only waiting on it, so
// one death produces one finding, on the node that was running.
func TestOrphanBlamesTheInnermostOpenSpan(t *testing.T) {
	st := openTemp(t)
	now := time.Now()
	sampled(st, t, "root", now.Add(-40*time.Second))
	sampled(st, t, "inner", now.Add(-20*time.Second))
	a := analyzerOn(t, st)
	f, err := a.orphanRun(context.Background(), "r1", now)
	if err != nil {
		t.Fatalf("orphanRun: %v", err)
	}
	if f == nil || ParseDetail(*f).UnreportedSpan != "inner" {
		t.Fatalf("finding = %+v, want the most recently sampled span blamed", f)
	}
	if d := ParseDetail(*f); d.Evidence == "" {
		t.Errorf("detail = %+v, want evidence naming the gap", d)
	}
}

func TestOrphanNamesTheSpanWhenItDidArrive(t *testing.T) {
	st := openTemp(t)
	now := time.Now()
	if err := st.WriteBatch(context.Background(), store.Batch{
		Source: "test",
		Spans: []store.Span{{
			ID: "open", RunID: "r1", Kind: store.KindTool, Name: "embed_corpus",
			StartedAt: now.Add(-time.Minute), Status: "ok",
		}},
	}); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	sampled(st, t, "open", now.Add(-30*time.Second))
	a := analyzerOn(t, st)
	f, err := a.orphanRun(context.Background(), "r1", now)
	if err != nil {
		t.Fatalf("orphanRun: %v", err)
	}
	if f == nil {
		t.Fatal("no finding for a started span that stopped reporting")
	}
	if got := FindingSummary(*f); !strings.Contains(got, "embed_corpus") {
		t.Errorf("summary = %q, want the span name in it", got)
	}
}

func TestSweepWritesOrphansWithNoFreshSpans(t *testing.T) {
	st := openTemp(t)
	sampled(st, t, "ghost", time.Now().Add(-time.Minute))
	a := analyzerOn(t, st)
	if err := a.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	found, err := st.Findings(context.Background(), "r1")
	if err != nil {
		t.Fatalf("Findings: %v", err)
	}
	if len(found) != 1 || found[0].Type != "orphaned_span" {
		t.Fatalf("findings = %+v, want one orphaned_span from a sweep with no new spans", found)
	}
}

func TestSweepDoesNotDuplicateAnOrphan(t *testing.T) {
	st := openTemp(t)
	sampled(st, t, "ghost", time.Now().Add(-time.Minute))
	a := analyzerOn(t, st)
	ctx := context.Background()
	for range 3 {
		if err := a.Sweep(ctx); err != nil {
			t.Fatalf("Sweep: %v", err)
		}
	}
	found, err := st.Findings(ctx, "r1")
	if err != nil {
		t.Fatalf("Findings: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("got %d findings after three sweeps, want 1", len(found))
	}
}

// The detail is part of a finding's identity, so anything in it that moves
// with the clock files the same death again on every tick.
func TestOrphanDetailDoesNotMoveWithTheClock(t *testing.T) {
	st := openTemp(t)
	now := time.Now()
	sampled(st, t, "ghost", now.Add(-time.Minute))
	a := analyzerOn(t, st)
	first, err := a.orphanRun(context.Background(), "r1", now)
	if err != nil {
		t.Fatalf("orphanRun: %v", err)
	}
	later, err := a.orphanRun(context.Background(), "r1", now.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("orphanRun: %v", err)
	}
	if first.Detail != later.Detail {
		t.Errorf("detail changed with the check time:\n%s\n%s", first.Detail, later.Detail)
	}
}
