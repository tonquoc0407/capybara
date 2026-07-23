package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/tonquoc0407/capybara/internal/export"
	"github.com/tonquoc0407/capybara/internal/store"
)

func exportCmd(ctx context.Context, dbPath string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	golden := fs.Bool("golden", false, "snapshot a known-good run as a CI fixture")
	dir := fs.String("o", export.DefaultDir, "output directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*golden || fs.NArg() != 1 {
		return errors.New("usage: capybara export --golden <run>")
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	run, err := st.ResolveRunID(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	fx, err := export.BuildFixture(ctx, st, run)
	if err != nil {
		return err
	}
	path, err := export.WriteGolden(*dir, fx)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, path)
	return err
}
