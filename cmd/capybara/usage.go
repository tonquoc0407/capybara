package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

func usageCmd(ctx context.Context, dbPath string, args []string, out io.Writer) error {
	if len(args) > 0 && args[0] == "window" {
		return usageWindowCmd(ctx, dbPath, args[1:], out)
	}
	if len(args) > 1 {
		return errors.New("usage: capybara usage [run]")
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	if len(args) == 1 {
		id, err := st.ResolveRunID(ctx, args[0])
		if err != nil {
			return err
		}
		provenance, err := st.CostProvenanceForRun(ctx, id)
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(provenance)
	}
	summary, err := st.UsageByProject(ctx)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(summary)
}

func usageWindowCmd(ctx context.Context, dbPath string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("usage window", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	project := fs.String("project", "default", "project")
	environment := fs.String("environment", "", "environment")
	release := fs.String("release", "", "release")
	from := fs.String("from", "", "RFC3339 range start")
	to := fs.String("to", "", "RFC3339 exclusive range end")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: capybara usage window [-project name] [-environment name] [-release version] [-from RFC3339] [-to RFC3339]")
	}
	f := store.OperationsFilter{Project: *project, Environment: *environment, Release: *release, To: time.Now().UTC()}
	for _, part := range []struct {
		name, raw string
		dst       *time.Time
	}{{"to", *to, &f.To}, {"from", *from, &f.From}} {
		if part.raw != "" {
			at, err := time.Parse(time.RFC3339, part.raw)
			if err != nil {
				return fmt.Errorf("invalid %s; expected RFC3339", part.name)
			}
			*part.dst = at
		}
	}
	if f.From.IsZero() {
		f.From = f.To.Add(-24 * time.Hour)
	}
	if err := f.Validate(); err != nil {
		return err
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	report, err := st.Operations(ctx, f)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(report)
}
