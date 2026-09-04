package store

import (
	"context"
	"testing"
	"time"
)

func sample(spanID string, at time.Time, cpu float64, rss int64) ResourceSample {
	return ResourceSample{RunID: "r1", SpanID: spanID, At: at, CPUUtil: &cpu, RSSBytes: &rss}
}

func TestPutResourceSamplesCreatesTheRunItself(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.PutResourceSamples(ctx, "test", []ResourceSample{
		sample("sp1", t0, 0.5, 1024),
	}); err != nil {
		t.Fatalf("PutResourceSamples: %v", err)
	}
	runs, err := s.ListRuns(ctx)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != "r1" {
		t.Fatalf("runs = %+v, want the sampled run to exist", runs)
	}
}

// The span a crash interrupted never reaches the spans table, so a sample must
// not require one.
func TestPutResourceSamplesKeepsSamplesForSpansThatNeverArrive(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.PutResourceSamples(ctx, "test", []ResourceSample{
		sample("ghost", t0, 1.5, 2048),
	}); err != nil {
		t.Fatalf("PutResourceSamples: %v", err)
	}
	latest, err := s.LatestResourceSamples(ctx, "r1")
	if err != nil {
		t.Fatalf("LatestResourceSamples: %v", err)
	}
	got, ok := latest["ghost"]
	if !ok {
		t.Fatal("sample for an unrecorded span was dropped")
	}
	if got.CPUUtil == nil || *got.CPUUtil != 1.5 {
		t.Errorf("cpu = %v, want 1.5 (a fraction over 1 on multiple cores)", got.CPUUtil)
	}
	if got.RSSBytes == nil || *got.RSSBytes != 2048 {
		t.Errorf("rss = %v, want 2048", got.RSSBytes)
	}
}

func TestLatestResourceSamplesKeepsTheNewestPerSpan(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.PutResourceSamples(ctx, "test", []ResourceSample{
		sample("sp1", t0, 0.1, 100),
		sample("sp1", t0.Add(2*time.Second), 0.9, 900),
		sample("sp2", t0.Add(time.Second), 0.4, 400),
	}); err != nil {
		t.Fatalf("PutResourceSamples: %v", err)
	}
	latest, err := s.LatestResourceSamples(ctx, "r1")
	if err != nil {
		t.Fatalf("LatestResourceSamples: %v", err)
	}
	if len(latest) != 2 {
		t.Fatalf("got %d spans, want 2", len(latest))
	}
	if *latest["sp1"].RSSBytes != 900 {
		t.Errorf("sp1 rss = %d, want the newest reading 900", *latest["sp1"].RSSBytes)
	}
	if !latest["sp1"].At.Equal(t0.Add(2 * time.Second)) {
		t.Errorf("sp1 at = %v, want the newest timestamp", latest["sp1"].At)
	}
}

func TestSampledSpansReportsWhetherTheSpanEverClosed(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.WriteBatch(ctx, testBatch()); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	if err := s.PutResourceSamples(ctx, "test", []ResourceSample{
		sample("llm1", t0, 0.2, 200),
		sample("ghost", t0.Add(time.Second), 0.8, 800),
	}); err != nil {
		t.Fatalf("PutResourceSamples: %v", err)
	}
	sampled, err := s.SampledSpans(ctx, "r1")
	if err != nil {
		t.Fatalf("SampledSpans: %v", err)
	}
	if len(sampled) != 2 {
		t.Fatalf("got %d sampled spans, want 2", len(sampled))
	}
	if !sampled[0].Ended || sampled[0].SpanID != "llm1" {
		t.Errorf("first = %+v, want the closed llm1 first (oldest reading)", sampled[0])
	}
	if sampled[1].Ended || sampled[1].SpanID != "ghost" {
		t.Errorf("second = %+v, want the unclosed ghost", sampled[1])
	}
	if !sampled[1].LastSample.Equal(t0.Add(time.Second)) {
		t.Errorf("ghost last sample = %v, want t0+1s", sampled[1].LastSample)
	}
}

func TestPutResourceSamplesEmptyIsANoop(t *testing.T) {
	s := openTemp(t)
	if err := s.PutResourceSamples(context.Background(), "test", nil); err != nil {
		t.Fatalf("PutResourceSamples(nil): %v", err)
	}
}

// A span ending between the two gauge callbacks leaves the last row holding
// only one of them; reading that row wholesale hid the other's whole history.
func TestLatestResourceSamplesFillsFromAHalfEmptyLastRow(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	cpu, rss := 0.8, int64(288*1024*1024)
	lateCPU := 0.02
	if err := s.PutResourceSamples(ctx, "test", []ResourceSample{
		{RunID: "r1", SpanID: "sp1", At: t0, CPUUtil: &cpu, RSSBytes: &rss},
		{RunID: "r1", SpanID: "sp1", At: t0.Add(time.Second), CPUUtil: &lateCPU},
	}); err != nil {
		t.Fatalf("PutResourceSamples: %v", err)
	}
	latest, err := s.LatestResourceSamples(ctx, "r1")
	if err != nil {
		t.Fatalf("LatestResourceSamples: %v", err)
	}
	got := latest["sp1"]
	if got.CPUUtil == nil || *got.CPUUtil != lateCPU {
		t.Errorf("cpu = %v, want the newest reading %v", got.CPUUtil, lateCPU)
	}
	if got.RSSBytes == nil || *got.RSSBytes != rss {
		t.Errorf("rss = %v, want the last one actually recorded, not nil", got.RSSBytes)
	}
}
