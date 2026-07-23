package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/tonquoc0407/capybara/internal/export"
	"github.com/tonquoc0407/capybara/internal/store"
	"github.com/tonquoc0407/capybara/internal/web"
)

func exportCmd(ctx context.Context, dbPath string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	golden := fs.Bool("golden", false, "snapshot a known-good run as a CI fixture")
	html := fs.Bool("html", false, "write the run as a self-contained page")
	dir := fs.String("o", "", "output directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 || (*golden && *html) {
		return errors.New("usage: capybara export [--golden | --html] <run>")
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
	if *html {
		// A page is meant to be sent to someone, not collected with the
		// test fixtures, so it lands where the command was run.
		path, err := web.WriteHTML(ctx, st, run, orDefault(*dir, "."))
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, path)
		return err
	}
	fx, err := export.BuildFixture(ctx, st, run)
	if err != nil {
		return err
	}
	if *golden {
		path, err := export.WriteGolden(orDefault(*dir, export.DefaultDir), fx)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, path)
		return err
	}
	paths, err := export.WritePytest(orDefault(*dir, export.DefaultDir), fx)
	if err != nil {
		return err
	}
	w := &errWriter{w: out}
	for _, path := range paths {
		w.printf("%s\n", path)
	}
	return w.err
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
