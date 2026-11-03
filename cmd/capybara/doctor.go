package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/tonquoc0407/capybara/internal/store"
)

func doctorCmd(ctx context.Context, dbPath string, args []string, out io.Writer) error {
	if len(args) != 0 {
		return errors.New("usage: capybara doctor")
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	d, err := st.Diagnostics(ctx)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "database: %s (ready)\nruns: %d  spans: %d  linked logs: %d\nunmapped spans: %d  llm spans without usage: %d  llm spans without cost: %d\n",
		dbPath, d.Runs, d.Spans, d.Logs, d.Unmapped, d.MissingUsage, d.MissingCost)
	return err
}
