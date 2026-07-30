package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/tonquoc0407/capybara/internal/store"
)

// runFilter selects runs by their columns and, when finding is set, by carrying
// a finding of that type. A zero field means that dimension is not filtered.
type runFilter struct {
	finding string
	model   string
	status  string
	source  string
	minCost float64
}

func (f runFilter) match(r store.Run, hasFinding bool) bool {
	if f.finding != "" && !hasFinding {
		return false
	}
	if f.model != "" && !strings.Contains(strings.ToLower(r.ModelMain), strings.ToLower(f.model)) {
		return false
	}
	if f.status != "" && r.Status != f.status {
		return false
	}
	if f.source != "" && r.Source != f.source {
		return false
	}
	if f.minCost > 0 && (r.CostUSD == nil || *r.CostUSD < f.minCost) {
		return false
	}
	return true
}

// runsCmd lists runs newest first, filtered, for scripting and terminal search.
func runsCmd(ctx context.Context, dbPath string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("runs", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	f := runFilter{}
	fs.StringVar(&f.finding, "finding", "", "only runs carrying a finding of this type")
	fs.StringVar(&f.model, "model", "", "only runs whose main model contains this")
	fs.StringVar(&f.status, "status", "", "only runs with this status")
	fs.StringVar(&f.source, "source", "", "only runs from this source")
	fs.Float64Var(&f.minCost, "min-cost", 0, "only runs costing at least this many usd")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	runs, err := st.ListRuns(ctx)
	if err != nil {
		return err
	}
	withFinding := make(map[string]bool)
	if f.finding != "" {
		all, err := st.AllFindings(ctx)
		if err != nil {
			return err
		}
		for _, fd := range all {
			if fd.Type == f.finding {
				withFinding[fd.RunID] = true
			}
		}
	}
	return printRuns(out, runs, f, withFinding)
}

func printRuns(out io.Writer, runs []store.Run, f runFilter, withFinding map[string]bool) error {
	w := &errWriter{w: out}
	for _, r := range runs {
		if !f.match(r, withFinding[r.ID]) {
			continue
		}
		w.printf("%-8s %-7s %5d %10s  %s\n",
			short8(r.ID), r.Status, r.Findings, cost(r.CostUSD), r.ModelMain)
	}
	return w.err
}

// cost prints four decimals: sub-cent runs are common and a bare $0.00 hides them.
func cost(c *float64) string {
	if c == nil {
		return "-"
	}
	return fmt.Sprintf("$%.4f", *c)
}
