package main

import (
	"testing"

	"github.com/tonquoc0407/capybara/internal/analyze"
)

func TestBelowThreshold(t *testing.T) {
	perfect := analyze.Score{Type: "improvised", TP: 3}
	missed := analyze.Score{Type: "loop", FN: 2}           // recall 0, no prediction
	unexercised := analyze.Score{Type: "drift"}            // never labelled, F1 undefined
	weak := analyze.Score{Type: "truncated", TP: 1, FP: 3} // F1 0.4
	cases := []struct {
		name   string
		scores []analyze.Score
		min    float64
		want   bool
	}{
		{"off by default", []analyze.Score{missed}, 0, false},
		{"all perfect passes", []analyze.Score{perfect}, 0.9, false},
		{"missed type counts as zero", []analyze.Score{perfect, missed}, 0.9, true},
		{"unexercised type is skipped", []analyze.Score{perfect, unexercised}, 0.9, false},
		{"weak score trips it", []analyze.Score{weak}, 0.5, true},
	}
	for _, c := range cases {
		if got := belowThreshold(c.scores, c.min); got != c.want {
			t.Errorf("%s: belowThreshold = %v, want %v", c.name, got, c.want)
		}
	}
}
