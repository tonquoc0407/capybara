package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/store"
)

// errBelowThreshold ends the process non-zero after the scores have printed, so
// a CI step fails when a detector's F1 regresses under --fail-under.
var errBelowThreshold = errors.New("below threshold")

// evalCmd re-analyzes a labelled corpus with the current detectors and reports
// precision and recall per finding type. It resets the store's findings first,
// so the numbers reflect the code in this build, not a stale sweep.
func evalCmd(ctx context.Context, dbPath string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	failUnder := fs.Float64("fail-under", 0, "exit non-zero if any exercised type scores below this F1")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: capybara eval [--fail-under f1] <labels.json>")
	}
	labels, err := analyze.ReadEvalLabels(fs.Arg(0))
	if err != nil {
		return err
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	an, err := analyze.New(st)
	if err != nil {
		return err
	}
	if err := st.ResetAnalysis(ctx); err != nil {
		return err
	}
	if err := an.Sweep(ctx); err != nil {
		return err
	}
	actual := make(map[string]map[string]bool, len(labels.Cases))
	for _, c := range labels.Cases {
		run, err := st.ResolveRunID(ctx, c.Run)
		if err != nil {
			return fmt.Errorf("eval case %q: %w", c.Run, err)
		}
		fs, err := st.Findings(ctx, run)
		if err != nil {
			return err
		}
		set := make(map[string]bool, len(fs))
		for _, f := range fs {
			set[f.Type] = true
		}
		actual[c.Run] = set
	}
	scores := analyze.Eval(labels, actual)
	if err := printEval(out, scores); err != nil {
		return err
	}
	if belowThreshold(scores, *failUnder) {
		return errBelowThreshold
	}
	return nil
}

// belowThreshold reports whether any type the corpus exercised (it has labelled
// positives) scored under min F1. A missed type has no precision and so no F1;
// that is the worst regression, not an exemption, so it counts as zero.
func belowThreshold(scores []analyze.Score, threshold float64) bool {
	if threshold <= 0 {
		return false
	}
	for _, s := range scores {
		if s.TP+s.FN == 0 {
			continue
		}
		f1, ok := s.F1()
		if !ok {
			f1 = 0
		}
		if f1 < threshold {
			return true
		}
	}
	return false
}

func printEval(out io.Writer, scores []analyze.Score) error {
	w := &errWriter{w: out}
	w.printf("%-16s %6s %6s %6s %6s %6s %6s %6s\n",
		"type", "runs", "caught", "missed", "false", "prec", "recall", "f1")
	for _, s := range scores {
		w.printf("%-16s %6d %6d %6d %6d %6s %6s %6s\n",
			s.Type, s.TP+s.FN, s.TP, s.FN, s.FP,
			ratio(s.Precision()), ratio(s.Recall()), ratio(s.F1()))
	}
	return w.err
}

// ratio renders a metric, or a dash when it is undefined (no run exercised it).
func ratio(v float64, ok bool) string {
	if !ok {
		return "-"
	}
	return fmt.Sprintf("%.2f", v)
}
