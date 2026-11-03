package store

import (
	"context"
	"testing"
	"time"
)

func TestRunAlertWindowCountsErrorsCostAndNearestRankLatency(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	for i, d := range []time.Duration{time.Second, 2 * time.Second, 3 * time.Second} {
		name := string(rune('a' + i))
		cost := float64(i+1) / 10
		status := "ok"
		if i == 2 {
			status = "error"
		}
		sp := Span{ID: "s" + name, RunID: "r" + name, Kind: KindAgent,
			Name: "agent", StartedAt: t0.Add(time.Duration(i) * time.Minute),
			EndedAt: t0.Add(time.Duration(i)*time.Minute + d), Status: status,
			CostUSD: &cost}
		if err := s.WriteBatch(ctx, Batch{Source: "test", Spans: []Span{sp}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.WriteBatch(ctx, Batch{Source: "test", Spans: []Span{
		{ID: "running-root", RunID: "running", Kind: KindAgent, Name: "agent", StartedAt: t0, Status: "running"},
		{ID: "running-child", RunID: "running", ParentID: "running-root", Kind: KindOther,
			Name: "closed child", StartedAt: t0, EndedAt: t0.Add(time.Hour), Status: "ok"},
	}}); err != nil {
		t.Fatal(err)
	}
	w, err := s.RunAlertWindow(ctx, "default", t0.Add(-time.Minute))
	if err != nil || w.Runs != 4 || w.Errors != 1 || w.P95LatencyMS != 3000 || w.UnpricedRuns != 1 || w.CostUSD < 0.599 || w.CostUSD > 0.601 {
		t.Fatalf("unexpected window: %+v, %v", w, err)
	}
	w, err = s.RunAlertWindow(ctx, "default", t0.Add(90*time.Second))
	if err != nil || w.Runs != 1 || w.Errors != 1 || w.P95LatencyMS != 3000 {
		t.Fatalf("window cutoff failed: %+v, %v", w, err)
	}
}
