package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/tonquoc0407/capybara/internal/store"
)

func pruneCmd(ctx context.Context, dbPath string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("prune", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	keep := fs.Int("keep", 0, "retain only the N newest runs")
	olderThan := fs.Duration("older-than", 0, "delete runs started older than this duration (e.g. 168h, 24h)")
	cleanOK := fs.Bool("clean-ok", false, "delete runs whose status is ok")
	dryRun := fs.Bool("dry-run", false, "report runs to delete without removing them")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: capybara prune [-keep N] [-older-than duration] [-clean-ok] [-dry-run]")
	}
	if *keep <= 0 && *olderThan <= 0 && !*cleanOK {
		return errors.New("specify at least one of -keep, -older-than, or -clean-ok")
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	deleted, err := st.Prune(ctx, store.PruneOptions{
		Keep:      *keep,
		OlderThan: *olderThan,
		CleanOK:   *cleanOK,
		DryRun:    *dryRun,
	})
	if err != nil {
		return err
	}
	if *dryRun {
		_, err = fmt.Fprintf(out, "would prune %d run(s)\n", deleted)
		return err
	}
	_, err = fmt.Fprintf(out, "pruned %d run(s)\n", deleted)
	return err
}

func vacuumCmd(ctx context.Context, dbPath string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("vacuum", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: capybara vacuum")
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	return st.Vacuum(ctx)
}

