package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/replay"
	"github.com/tonquoc0407/capybara/internal/store"
)

func replayCmd(ctx context.Context, dbPath string, capture bool, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("replay", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	spanID := fs.String("span", "", "span whose recorded output to replace")
	outputFile := fs.String("output", "", "file holding the replacement output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: capybara replay [-span id -output file] <run>")
	}
	override := ""
	if *outputFile != "" {
		raw, err := os.ReadFile(*outputFile)
		if err != nil {
			return fmt.Errorf("read override: %w", err)
		}
		override = string(raw)
	}
	if override != "" && *spanID == "" {
		return errors.New("-output needs -span")
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	parent, err := st.ResolveRunID(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	m, err := replay.Build(ctx, st, parent, *spanID, override)
	if err != nil {
		return err
	}
	if err := replay.Run(ctx, st, m, capture); err != nil {
		return err
	}
	an, err := analyze.New(st)
	if err != nil {
		return err
	}
	if err := an.Sweep(ctx); err != nil {
		return fmt.Errorf("analyze replay: %w", err)
	}
	_, err = fmt.Fprintln(out, m.RunID)
	return err
}
