package analyze

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// EvalLabels is a corpus's ground truth. Types names the finding types to
// score; anything else the analyzer emits is ignored, so a corpus can measure
// one detector without labelling every incidental finding it shares a run with.
type EvalLabels struct {
	Types []string   `json:"types"`
	Cases []EvalCase `json:"cases"`
}

// EvalCase labels one run by the scored finding types it should carry. An empty
// Expect means the run must stay clean.
type EvalCase struct {
	Run    string   `json:"run"`
	Expect []string `json:"expect"`
}

// Score is one finding type's confusion counts over the corpus: a run is the
// unit, so TP is a labelled run the detector flagged, FP a clean run it flagged,
// FN a labelled run it missed.
type Score struct {
	Type       string
	TP, FP, FN int
}

// Precision is TP/(TP+FP); the bool is false when nothing was predicted.
func (s Score) Precision() (float64, bool) {
	if s.TP+s.FP == 0 {
		return 0, false
	}
	return float64(s.TP) / float64(s.TP+s.FP), true
}

// Recall is TP/(TP+FN); the bool is false when there were no positives.
func (s Score) Recall() (float64, bool) {
	if s.TP+s.FN == 0 {
		return 0, false
	}
	return float64(s.TP) / float64(s.TP+s.FN), true
}

// F1 is the harmonic mean of precision and recall, the field-standard single
// number for an imbalanced detector where accuracy would flatter a corpus of
// mostly clean runs. The bool is false when either input is undefined.
func (s Score) F1() (float64, bool) {
	p, okp := s.Precision()
	r, okr := s.Recall()
	if !okp || !okr || p+r == 0 {
		return 0, false
	}
	return 2 * p * r / (p + r), true
}

// ReadEvalLabels loads a corpus's ground-truth labels from a JSON file.
func ReadEvalLabels(path string) (EvalLabels, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return EvalLabels{}, fmt.Errorf("read labels: %w", err)
	}
	var l EvalLabels
	if err := json.Unmarshal(raw, &l); err != nil {
		return EvalLabels{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return l, nil
}

// Eval scores the analyzer against labelled runs. actual maps a case's run
// identifier to the set of finding types the analyzer produced for that run.
func Eval(labels EvalLabels, actual map[string]map[string]bool) []Score {
	types := scoredTypes(labels)
	byType := make(map[string]*Score, len(types))
	for _, t := range types {
		byType[t] = &Score{Type: t}
	}
	for _, c := range labels.Cases {
		want := toSet(c.Expect)
		got := actual[c.Run]
		for _, t := range types {
			sc := byType[t]
			switch {
			case want[t] && got[t]:
				sc.TP++
			case want[t]:
				sc.FN++
			case got[t]:
				sc.FP++
			}
		}
	}
	out := make([]Score, 0, len(types))
	for _, t := range types {
		out = append(out, *byType[t])
	}
	return out
}

// scoredTypes is the explicit Types list, or the union of every expected type
// when the corpus does not name one.
func scoredTypes(labels EvalLabels) []string {
	set := toSet(labels.Types)
	if len(set) == 0 {
		for _, c := range labels.Cases {
			for _, t := range c.Expect {
				set[t] = true
			}
		}
	}
	types := make([]string, 0, len(set))
	for t := range set {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

func toSet(items []string) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, it := range items {
		set[it] = true
	}
	return set
}
