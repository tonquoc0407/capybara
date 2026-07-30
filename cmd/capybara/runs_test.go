package main

import (
	"testing"

	"github.com/tonquoc0407/capybara/internal/store"
)

func f64(v float64) *float64 { return &v }

func TestRunFilterMatch(t *testing.T) {
	r := store.Run{ID: "r1", Status: "ok", Source: "otlp", ModelMain: "claude-opus-4-8", CostUSD: f64(0.02)}
	cases := []struct {
		name   string
		filter runFilter
		has    bool
		want   bool
	}{
		{"empty matches", runFilter{}, false, true},
		{"model substring", runFilter{model: "opus"}, false, true},
		{"model case-insensitive", runFilter{model: "OPUS"}, false, true},
		{"model miss", runFilter{model: "gpt"}, false, false},
		{"status match", runFilter{status: "ok"}, false, true},
		{"status miss", runFilter{status: "error"}, false, false},
		{"source match", runFilter{source: "otlp"}, false, true},
		{"min-cost under", runFilter{minCost: 0.05}, false, false},
		{"min-cost over", runFilter{minCost: 0.01}, false, true},
		{"finding needs flag", runFilter{finding: "improvised"}, false, false},
		{"finding with flag", runFilter{finding: "improvised"}, true, true},
	}
	for _, c := range cases {
		if got := c.filter.match(r, c.has); got != c.want {
			t.Errorf("%s: match = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestRunFilterNilCostNeverMeetsMin(t *testing.T) {
	r := store.Run{ID: "r1", Status: "ok", CostUSD: nil}
	f := runFilter{minCost: 0.001}
	if f.match(r, false) {
		t.Error("nil cost must not satisfy a min-cost filter")
	}
}
