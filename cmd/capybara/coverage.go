package main

import (
	"context"
	"errors"
	"io"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/store"
)

// kindOrder fixes the report's rows so the taxonomy reads the same every run.
var kindOrder = []store.Kind{
	store.KindLLM, store.KindTool, store.KindAgent, store.KindRetrieval, store.KindOther,
}

// coverageCmd reports how much of the store capybara typed and which attribute
// namespaces on the untyped remainder point at an ingest convention it misses.
func coverageCmd(ctx context.Context, dbPath string, args []string, out io.Writer) error {
	if len(args) > 1 {
		return errors.New("usage: capybara coverage [run]")
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	var spans []store.Span
	if len(args) == 1 {
		run, err := st.ResolveRunID(ctx, args[0])
		if err != nil {
			return err
		}
		spans, err = st.Spans(ctx, run)
		if err != nil {
			return err
		}
	} else if spans, err = st.AllSpans(ctx); err != nil {
		return err
	}
	return printCoverage(out, analyze.SpanCoverage(spans))
}

func printCoverage(out io.Writer, cov analyze.Coverage) error {
	w := &errWriter{w: out}
	w.printf("%-12s %5d\n", "spans", cov.Total)
	for _, k := range kindOrder {
		if n := cov.ByKind[k]; n > 0 {
			w.printf("  %-10s %5d\n", k, n)
		}
	}
	if len(cov.Prefixes) > 0 {
		w.printf("unmapped namespaces on other spans (* = capybara maps it elsewhere):\n")
		for _, p := range cov.Prefixes {
			mark := " "
			if p.AI {
				mark = "*"
			}
			w.printf("%s %-10s %5d\n", mark, p.Prefix, p.Count)
		}
	}
	return w.err
}
