package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestOperationsKeepsLatencyCostAndQualityDenominatorsCorrect(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "ops.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	base := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	seed := func(id, project, environment, release, status string, at time.Time, duration time.Duration, cost *float64) {
		t.Helper()
		sp := Span{ID: id + "-root", RunID: id, Kind: KindAgent, Name: "agent", Status: status,
			StartedAt: at, EndedAt: at.Add(duration), CostUSD: cost}
		spans := []Span{sp}
		if status == "running" {
			spans[0].EndedAt = time.Time{}
			spans = append(spans, Span{ID: id + "-child", RunID: id, ParentID: sp.ID, Kind: KindOther,
				Name: "closed child", Status: "ok", StartedAt: at, EndedAt: at.Add(999 * time.Second)})
		}
		if id == "run-0" {
			spans = append(spans, Span{ID: "unpriced-llm", RunID: id, ParentID: sp.ID, Kind: KindLLM,
				Name: "unknown model", Status: "ok", StartedAt: at, EndedAt: at.Add(time.Millisecond)})
		}
		if err := st.WriteBatch(ctx, Batch{Source: "test", Spans: spans,
			RunContexts: map[string]RunContext{id: {Project: project, Environment: environment, Release: release}}}); err != nil {
			t.Fatal(err)
		}
	}
	one := 1.0
	for i := 0; i < 20; i++ {
		release, status := "v1", "ok"
		if i >= 10 {
			release = "v2"
		}
		if i == 0 {
			status = "error"
		}
		seed(fmt.Sprintf("run-%d", i), "p", "prod", release, status, base.Add(time.Duration(i)*time.Hour), time.Duration(i+1)*time.Second, &one)
	}
	seed("running", "p", "prod", "v1", "running", base.Add(21*time.Hour), 0, nil)
	seed("invalid", "p", "prod", "v2", "ok", base.Add(22*time.Hour), -time.Second, nil)
	seed("boundary", "p", "prod", "v1", "error", base.Add(24*time.Hour), time.Second, &one)
	seed("other-project", "private", "prod", "v1", "error", base, time.Hour, &one)
	seed("other-env", "p", "test", "v1", "error", base, time.Hour, &one)
	for _, value := range []float64{0.2, 0.8} {
		if _, err := st.AddScore(ctx, "run-0", "", "quality", value, "human", ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.AddScore(ctx, "run-0", "", "safety", 0.4, "human", ""); err != nil {
		t.Fatal(err)
	}
	f := OperationsFilter{Project: "p", Environment: "prod", From: base, To: base.Add(24 * time.Hour)}
	report, err := st.Operations(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	g := report.Total
	if g.Runs != 22 || g.Errors != 1 || g.Running != 1 || g.Completed != 20 || g.KnownCostUSD != 20 ||
		g.UnpricedRuns != 2 || g.UnpricedLLMSpans != 1 || g.P95LatencyMS == nil || *g.P95LatencyMS != 19000 {
		t.Fatalf("wrong totals: %+v", g)
	}
	if len(report.Releases) != 2 || len(report.Hourly) != 22 || len(report.Quality) != 2 {
		t.Fatalf("wrong groups: %+v", report)
	}
	for _, q := range report.Quality {
		want := 0.4
		if q.Name == "quality" {
			want = 0.8
		}
		if q.Average != want || q.Samples != 1 || q.ScoredRuns != 1 || q.Release != "v1" {
			t.Fatalf("latest metric was mixed or duplicated: %+v", q)
		}
	}
	f.Release = "v1"
	report, err = st.Operations(ctx, f)
	if err != nil || report.Total.Runs != 11 || *report.Total.P95LatencyMS != 10000 {
		t.Fatalf("release filter: %+v, %v", report, err)
	}
	f.Project = "empty"
	report, err = st.Operations(ctx, f)
	if err != nil || report.Total.Runs != 0 || report.Total.P95LatencyMS != nil || len(report.Hourly) != 0 {
		t.Fatalf("empty report: %+v, %v", report, err)
	}
	f.To = f.From.Add(32 * 24 * time.Hour)
	if _, err := st.Operations(ctx, f); err == nil {
		t.Fatal("unbounded time range accepted")
	}
}
