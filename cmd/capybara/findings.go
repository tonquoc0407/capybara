package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"strings"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/export"
	"github.com/tonquoc0407/capybara/internal/store"
)

// errFindings ends the process non-zero after the report has printed, so a CI
// step fails on a matched finding without a second error line.
var errFindings = errors.New("findings")

// findingsCmd lists a corpus's findings for CI: text by default, SARIF for
// interchange, and a non-zero exit when a finding matches --fail-on.
func findingsCmd(ctx context.Context, dbPath string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("findings", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asSarif := fs.Bool("sarif", false, "emit SARIF 2.1.0 instead of text")
	failOn := fs.String("fail-on", "", "comma-separated types (or 'any') to exit non-zero on")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return errors.New("usage: capybara findings [--sarif] [--fail-on types] [run]")
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
	if err := an.Sweep(ctx); err != nil {
		return err
	}
	findings, err := gatherFindings(ctx, st, fs.Args())
	if err != nil {
		return err
	}
	if *asSarif {
		doc, err := export.SARIF(findings, version)
		if err != nil {
			return err
		}
		if _, err := out.Write(append(doc, '\n')); err != nil {
			return err
		}
	} else if err := printFindings(out, findings); err != nil {
		return err
	}
	if breached(findings, *failOn) {
		return errFindings
	}
	return nil
}

func gatherFindings(ctx context.Context, st *store.Store, args []string) ([]store.Finding, error) {
	if len(args) == 1 {
		run, err := st.ResolveRunID(ctx, args[0])
		if err != nil {
			return nil, err
		}
		return st.Findings(ctx, run)
	}
	return st.AllFindings(ctx)
}

func printFindings(out io.Writer, findings []store.Finding) error {
	w := &errWriter{w: out}
	for _, f := range findings {
		w.printf("%-8s %s\n", short8(f.RunID), analyze.FindingLine(f))
	}
	return w.err
}

// breached reports whether any finding matches the fail set: "any" trips on the
// first finding, otherwise a comma list of types.
func breached(findings []store.Finding, failOn string) bool {
	spec := strings.TrimSpace(failOn)
	if spec == "" || len(findings) == 0 {
		return false
	}
	if spec == "any" {
		return true
	}
	want := make(map[string]bool)
	for _, t := range strings.Split(spec, ",") {
		if t = strings.TrimSpace(t); t != "" {
			want[t] = true
		}
	}
	for _, f := range findings {
		if want[f.Type] {
			return true
		}
	}
	return false
}

func short8(runID string) string {
	if len(runID) > 8 {
		return runID[:8]
	}
	return runID
}
