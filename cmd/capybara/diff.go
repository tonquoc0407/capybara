package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/store"
)

const diffBodyLimit = 2048

func diffCmd(ctx context.Context, dbPath string, args []string, out io.Writer) error {
	if len(args) != 2 {
		return errors.New("usage: capybara diff <run_a> <run_b>")
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	runA, err := st.ResolveRunID(ctx, args[0])
	if err != nil {
		return err
	}
	runB, err := st.ResolveRunID(ctx, args[1])
	if err != nil {
		return err
	}
	d, err := analyze.DiffRuns(ctx, st, runA, runB)
	if err != nil {
		return err
	}
	return printDiff(out, d)
}

func printDiff(out io.Writer, d *analyze.RunDiff) error {
	w := &errWriter{w: out}
	w.printf("diff %s %s\n\n", shortID(d.RunA), shortID(d.RunB))
	for i, step := range d.Steps {
		mark := " "
		if step.Diverged {
			mark = "*"
		}
		switch {
		case step.A == nil:
			w.printf("%s %-32s only in %s\n", mark, truncate(step.StepName(), 32), shortID(d.RunB))
		case step.B == nil:
			w.printf("%s %-32s only in %s\n", mark, truncate(step.StepName(), 32), shortID(d.RunA))
		default:
			w.printf("%s %-32s %8s -> %-8s tok %-7s %s\n",
				mark, truncate(step.StepName(), 32),
				durOrDash(step.A), durOrDash(step.B),
				signedInt(step.DTokens()), signedCost(step.DCost()))
		}
		if i == d.FirstDivergence {
			w.printf("  ^ first divergence\n")
		}
	}
	if d.FirstDivergence >= 0 {
		step := d.Steps[d.FirstDivergence]
		w.printf("\nfirst divergence: %s\n", step.StepName())
		w.printf("--- %s\n%s", shortID(d.RunA), sideContent(step.A, d.ContentsA))
		w.printf("+++ %s\n%s", shortID(d.RunB), sideContent(step.B, d.ContentsB))
	}
	return w.err
}

func sideContent(sp *store.Span, contents map[string][]store.Content) string {
	if sp == nil {
		return "(no step)\n"
	}
	rows := contents[sp.ID]
	if len(rows) == 0 {
		return "(no content recorded)\n"
	}
	var b strings.Builder
	for _, c := range rows {
		body := c.Body
		if len(body) > diffBodyLimit {
			body = body[:diffBodyLimit] + "…"
		}
		fmt.Fprintf(&b, "%s: %s\n", c.Role, body)
	}
	return b.String()
}

func durOrDash(sp *store.Span) string {
	if sp == nil || sp.StartedAt.IsZero() || sp.EndedAt.IsZero() {
		return "-"
	}
	return formatDur(sp.EndedAt.Sub(sp.StartedAt))
}

func formatDur(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
}

func signedInt(v int64) string {
	if v > 0 {
		return fmt.Sprintf("+%d", v)
	}
	return fmt.Sprintf("%d", v)
}

func signedCost(v *float64) string {
	if v == nil {
		return ""
	}
	if *v >= 0 {
		return fmt.Sprintf("$+%.4f", *v)
	}
	return fmt.Sprintf("$%.4f", *v)
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) printf(format string, args ...any) {
	if e.err != nil {
		return
	}
	_, e.err = fmt.Fprintf(e.w, format, args...)
}
