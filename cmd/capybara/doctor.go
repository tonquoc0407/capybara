package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/tonquoc0407/capybara/internal/store"
)

func doctorCmd(ctx context.Context, dbPath string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	bundle := fs.String("bundle", "", "write privacy-safe diagnostic JSON")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return fmt.Errorf("usage: capybara doctor [-bundle diagnostics.json]")
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
	if *bundle != "" {
		// Only aggregate counts and build identity are exported. DB paths,
		// environment variables, trace IDs and recorded content stay local.
		report := struct {
			Version string            `json:"version"`
			OS      string            `json:"os"`
			Arch    string            `json:"arch"`
			Counts  store.Diagnostics `json:"counts"`
		}{displayVersion(), runtime.GOOS, runtime.GOARCH, d}
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		data = append(data, '\n')
		file, err := os.OpenFile(*bundle, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		if _, err := file.Write(data); err != nil {
			file.Close()
			os.Remove(*bundle)
			return err
		}
		if err := file.Close(); err != nil {
			os.Remove(*bundle)
			return err
		}
		_, err = fmt.Fprintf(out, "diagnostics written to %s (aggregate counts and build info; no trace content)\n", *bundle)
		return err
	}
	_, err = fmt.Fprintf(out, "database: %s (ready)\nruns: %d  spans: %d  linked logs: %d\nunmapped spans: %d  llm spans without usage: %d  llm spans without cost: %d\n",
		dbPath, d.Runs, d.Spans, d.Logs, d.Unmapped, d.MissingUsage, d.MissingCost)
	return err
}
