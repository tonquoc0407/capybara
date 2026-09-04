package tui

import (
	"strings"
	"testing"
)

func TestBrailleGraphFillsFromTheBaseline(t *testing.T) {
	rows := brailleGraph([]float64{1, 1}, 1, 1, 2)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	// A full-height value lights every dot in both cells.
	for i, r := range rows {
		if r != "⣿" {
			t.Errorf("row %d = %q, want a fully lit cell", i, r)
		}
	}
}

func TestBrailleGraphLeavesTheTopClearForALowValue(t *testing.T) {
	rows := brailleGraph([]float64{0.1, 0.1}, 1, 1, 2)
	if rows[0] != "⠀" {
		t.Errorf("top row = %q, want it empty for a value near the floor", rows[0])
	}
	if rows[1] == "⠀" {
		t.Error("bottom row is empty; a non-zero value must still show")
	}
}

// A value too small to reach one pixel still has to register, or a busy-but-low
// reading is indistinguishable from idle.
func TestBrailleGraphNeverLosesATinyValue(t *testing.T) {
	rows := brailleGraph([]float64{0.0001}, 1, 1, 4)
	if strings.Trim(strings.Join(rows, ""), "⠀") == "" {
		t.Error("a tiny value rendered as nothing")
	}
}

func TestBrailleGraphIsEmptyForAZeroValue(t *testing.T) {
	rows := brailleGraph([]float64{0, 0}, 1, 1, 2)
	for i, r := range rows {
		if r != "⠀" {
			t.Errorf("row %d = %q, want empty for zero", i, r)
		}
	}
}

// A run that just started has little history; stretching it would invent a
// past it does not have, so the left stays blank.
func TestBrailleGraphRightAlignsShortHistory(t *testing.T) {
	rows := brailleGraph([]float64{1, 1}, 1, 4, 1)
	got := []rune(rows[0])
	if len(got) != 4 {
		t.Fatalf("row width = %d, want 4", len(got))
	}
	for i := range 3 {
		if got[i] != ' ' && got[i] != rune(brailleBase) {
			t.Errorf("column %d = %q, want blank left padding", i, string(got[i]))
		}
	}
	if got[3] == ' ' || got[3] == rune(brailleBase) {
		t.Error("newest sample must sit at the right edge")
	}
}

func TestBrailleGraphKeepsOnlyTheNewestWhenOverfull(t *testing.T) {
	values := make([]float64, 100)
	values[len(values)-1] = 1 // only the newest is non-zero
	rows := brailleGraph(values, 1, 2, 1)
	got := []rune(rows[0])
	if got[len(got)-1] == rune(brailleBase) {
		t.Error("the newest sample was dropped")
	}
}

func TestBrailleGraphHandlesNoScale(t *testing.T) {
	rows := brailleGraph([]float64{5}, 0, 3, 2)
	for _, r := range rows {
		if strings.TrimSpace(r) != "" {
			t.Errorf("row = %q, want blank when there is no scale", r)
		}
	}
}
