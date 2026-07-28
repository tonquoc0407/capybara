package analyze

import "testing"

func TestEvalScoresPerType(t *testing.T) {
	labels := EvalLabels{
		Types: []string{"improvised"},
		Cases: []EvalCase{
			{Run: "a", Expect: []string{"improvised"}}, // caught -> TP
			{Run: "b", Expect: []string{"improvised"}}, // missed -> FN
			{Run: "c", Expect: nil},                    // clean, flagged -> FP
			{Run: "d", Expect: nil},                    // clean, clean -> nothing
		},
	}
	actual := map[string]map[string]bool{
		"a": {"improvised": true},
		"b": {},
		"c": {"improvised": true},
		"d": {},
	}
	scores := Eval(labels, actual)
	if len(scores) != 1 {
		t.Fatalf("scores = %+v", scores)
	}
	s := scores[0]
	if s.TP != 1 || s.FN != 1 || s.FP != 1 {
		t.Fatalf("TP/FN/FP = %d/%d/%d, want 1/1/1", s.TP, s.FN, s.FP)
	}
	if p, ok := s.Precision(); !ok || p != 0.5 {
		t.Errorf("precision = %v (%v), want 0.5", p, ok)
	}
	if r, ok := s.Recall(); !ok || r != 0.5 {
		t.Errorf("recall = %v (%v), want 0.5", r, ok)
	}
	if f, ok := s.F1(); !ok || f != 0.5 {
		t.Errorf("f1 = %v (%v), want 0.5", f, ok)
	}
}

// A finding type the corpus does not score must not count, so a run can be
// labelled for one detector without enumerating every incidental finding.
func TestEvalIgnoresUnscoredTypes(t *testing.T) {
	labels := EvalLabels{
		Types: []string{"improvised"},
		Cases: []EvalCase{{Run: "a", Expect: []string{"improvised"}}},
	}
	actual := map[string]map[string]bool{"a": {"improvised": true, "tool_error": true}}
	scores := Eval(labels, actual)
	if len(scores) != 1 || scores[0].FP != 0 {
		t.Fatalf("unscored type leaked into scores: %+v", scores)
	}
}

// With no Types named, the scored set is the union of what the cases expect.
func TestEvalInfersScoredTypesFromCases(t *testing.T) {
	labels := EvalLabels{Cases: []EvalCase{
		{Run: "a", Expect: []string{"improvised"}},
		{Run: "b", Expect: []string{"loop"}},
	}}
	actual := map[string]map[string]bool{"a": {"improvised": true}, "b": {}}
	scores := Eval(labels, actual)
	if len(scores) != 2 || scores[0].Type != "improvised" || scores[1].Type != "loop" {
		t.Fatalf("inferred types = %+v", scores)
	}
}

func TestPrecisionRecallUndefinedWhenUnexercised(t *testing.T) {
	s := Score{Type: "x"}
	if _, ok := s.Precision(); ok {
		t.Error("precision defined with no predictions")
	}
	if _, ok := s.Recall(); ok {
		t.Error("recall defined with no positives")
	}
}
