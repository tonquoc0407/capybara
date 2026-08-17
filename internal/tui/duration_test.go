package tui

import (
	"testing"
	"time"
)

func TestHumanDurTiers(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{500 * time.Millisecond, "500ms"},
		{45*time.Second + 700*time.Millisecond, "45.7s"},
		{9*time.Minute + 5*time.Second, "9m05s"},
		{2*time.Hour + 3*time.Minute, "2h03m"},
		{3*24*time.Hour + 1*time.Hour, "3d01h"},
	}
	for _, c := range cases {
		if got := humanDur(c.d); got != c.want {
			t.Errorf("humanDur(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestHumanDurationMatchesHumanDur(t *testing.T) {
	// A resumed multi-day Claude Code session must not read as an unbounded
	// minute count.
	if got := humanDuration(96208.0); got != "1d02h" {
		t.Errorf("humanDuration(96208) = %q, want 1d02h", got)
	}
}
