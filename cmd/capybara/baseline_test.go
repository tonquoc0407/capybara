package main

import (
	"path/filepath"
	"testing"

	"github.com/tonquoc0407/capybara/internal/store"
)

func TestBaselineRoundTripAndNewFindings(t *testing.T) {
	known := []store.Finding{
		{RunID: "r1", SpanID: "s1", Type: "improvised"},
		{RunID: "r1", SpanID: "s2", Type: "loop"},
	}
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := writeBaseline(path, known); err != nil {
		t.Fatalf("writeBaseline: %v", err)
	}
	accepted, err := readBaseline(path)
	if err != nil {
		t.Fatalf("readBaseline: %v", err)
	}
	current := []store.Finding{
		{RunID: "r1", SpanID: "s1", Type: "improvised"},        // in baseline
		{RunID: "r1", SpanID: "s2", Type: "loop"},              // in baseline
		{RunID: "r1", SpanID: "s3", Type: "prompt_injection"},  // new span
		{RunID: "r1", SpanID: "s1", Type: "unsupported_claim"}, // new type, same span
	}
	got := newFindings(current, accepted)
	if len(got) != 2 {
		t.Fatalf("newFindings = %d, want 2: %+v", len(got), got)
	}
	if got[0].Type != "prompt_injection" || got[1].Type != "unsupported_claim" {
		t.Errorf("wrong regressions: %+v", got)
	}
}

func TestNewFindingsIgnoresDetailChanges(t *testing.T) {
	accepted := map[string]bool{identity(store.Finding{RunID: "r", SpanID: "s", Type: "drift"}): true}
	current := []store.Finding{{RunID: "r", SpanID: "s", Type: "drift", Detail: `{"missing":["x"]}`}}
	if got := newFindings(current, accepted); len(got) != 0 {
		t.Errorf("a detail edit is not a regression: %+v", got)
	}
}

func TestReadBaselineMissing(t *testing.T) {
	if _, err := readBaseline(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Error("a missing baseline must be an error, not an empty pass")
	}
}
