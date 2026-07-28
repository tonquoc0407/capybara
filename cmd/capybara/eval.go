package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/store"
)

// evalCmd re-analyzes a labelled corpus with the current detectors and reports
// precision and recall per finding type. It resets the store's findings first,
// so the numbers reflect the code in this build, not a stale sweep.
func evalCmd(ctx context.Context, dbPath string, args []string, out io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: capybara eval <labels.json>")
	}
	labels, err := analyze.ReadEvalLabels(args[0])
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
	return printEval(out, analyze.Eval(labels, actual))
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
