package main

import (
	"testing"
	"time"
)

func TestFormatDurTiers(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{500 * time.Millisecond, "500ms"},
		{9*time.Minute + 5*time.Second, "9m05s"},
		{2*time.Hour + 3*time.Minute, "2h03m"},
		{3*24*time.Hour + 1*time.Hour, "3d01h"},
	}
	for _, c := range cases {
		if got := formatDur(c.d); got != c.want {
			t.Errorf("formatDur(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
