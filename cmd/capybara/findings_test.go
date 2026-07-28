package main

import (
	"testing"

	"github.com/tonquoc0407/capybara/internal/store"
)

func TestBreached(t *testing.T) {
	fs := []store.Finding{{Type: "tool_error"}, {Type: "improvised"}}
	cases := []struct {
		failOn string
		want   bool
	}{
		{"", false},
		{"any", true},
		{"improvised", true},
		{"loop", false},
		{"loop,improvised", true},
		{" improvised , loop ", true},
	}
	for _, c := range cases {
		if got := breached(fs, c.failOn); got != c.want {
			t.Errorf("breached(%q) = %v, want %v", c.failOn, got, c.want)
		}
	}
}

func TestBreachedNoFindings(t *testing.T) {
	if breached(nil, "any") {
		t.Error("no findings must not breach even on 'any'")
	}
}
