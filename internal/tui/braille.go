package tui

// Braille cells carry a 2x4 dot grid, so one character row is four pixel rows
// and one character column is two samples. That is four times the vertical
// resolution of a block-character bar in the same space, which is what makes a
// six-row graph readable at all.
//
// Dot bits within a cell, left column then right:
//
//	0 3
//	1 4
//	2 5
//	6 7
var brailleDots = [2][4]uint8{
	{0, 1, 2, 6},
	{3, 4, 5, 7},
}

const brailleBase = 0x2800

// brailleGraph renders values as an area chart filled from the baseline, newest
// on the right. Fewer values than the graph is wide leaves the left blank
// rather than stretching, so a run that just started does not read as history
// it does not have.
func brailleGraph(values []float64, maxValue float64, width, height int) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	rows := make([]string, height)
	if maxValue <= 0 {
		blank := make([]rune, width)
		for i := range blank {
			blank[i] = ' '
		}
		for r := range rows {
			rows[r] = string(blank)
		}
		return rows
	}
	total := height * 4
	samples := width * 2
	levels := make([]int, samples)
	// Right-align: the newest sample sits at the right edge, and a short
	// history leaves the left columns empty.
	start := len(values) - samples
	for i := range levels {
		idx := start + i
		if idx < 0 || idx >= len(values) {
			levels[i] = -1
			continue
		}
		levels[i] = scaleLevel(values[idx], maxValue, total)
	}
	cells := make([][]rune, height)
	for r := range cells {
		cells[r] = make([]rune, width)
	}
	for r := range height {
		for c := range width {
			var bits uint8
			for col := range 2 {
				level := levels[c*2+col]
				if level < 0 {
					continue
				}
				for k := range 4 {
					if p := r*4 + k; total-p <= level {
						bits |= 1 << brailleDots[col][k]
					}
				}
			}
			cells[r][c] = rune(brailleBase + int(bits))
		}
	}
	for r := range height {
		rows[r] = string(cells[r])
	}
	return rows
}

// A non-zero value never renders as nothing: a barely-used core still has to
// show a floor, or the graph reads as idle when it is not.
func scaleLevel(v, maxValue float64, total int) int {
	if v <= 0 {
		return 0
	}
	level := int(v / maxValue * float64(total))
	if level < 1 {
		level = 1
	}
	return min(level, total)
}
