package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/tonquoc0407/capybara/internal/export"
	"github.com/tonquoc0407/capybara/internal/store"
)

// errDiverged ends the process with a non-zero status after the report has
// already been printed, so CI fails without a second error line.
var errDiverged = errors.New("diverged")

func checkCmd(ctx context.Context, dbPath string, args []string, out io.Writer) error {
	if len(args) != 2 {
		return errors.New("usage: capybara check <golden> <run>")
	}
	golden, err := export.ReadFixture(args[0])
	if err != nil {
		return err
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	run, err := st.ResolveRunID(ctx, args[1])
	if err != nil {
		return err
	}
	fx, err := export.BuildFixture(ctx, st, run)
	if err != nil {
		return err
	}
	if len(golden.Tools) == 0 && len(golden.Spans) == 0 && golden.Status == "" {
		return errors.New("empty golden: no tool calls or span outcomes to check")
	}
	return printCheck(out, len(golden.Spans), len(golden.Tools), export.Check(golden, fx))
}

// printCheck reports every divergence and stays silent on a clean run.
func printCheck(out io.Writer, spans, tools int, divs []export.Divergence) error {
	if len(divs) == 0 {
		return nil
	}
	w := &errWriter{w: out}
	for _, d := range divs {
		if d.Span != "" {
			w.printf("span %-12s %s\n", d.Span, d.Reason)
		} else {
			w.printf("tool %-12s %s\n", d.Tool, d.Reason)
		}
		if d.Golden != "" {
			w.printf("  golden  %s\n", truncate(oneLine(d.Golden), 60))
		}
		if d.Run != "" {
			w.printf("  run     %s\n", truncate(oneLine(d.Run), 60))
		}
		if d.Detail != "" {
			w.printf("  drift   %s\n", d.Detail)
		}
	}
	w.printf("%s and %s checked, %s\n",
		plural(spans, "span"), plural(tools, "call"), plural(len(divs), "divergence"))
	if w.err != nil {
		return w.err
	}
	return errDiverged
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
