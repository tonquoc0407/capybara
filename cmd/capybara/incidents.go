package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/tonquoc0407/capybara/internal/incident"
	"github.com/tonquoc0407/capybara/internal/store"
)

func incidentsCmd(ctx context.Context, dbPath string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("incidents", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	project := fs.String("project", "", "project to inspect")
	limit := fs.Int("limit", 100, "number of recent runs to inspect (1-100)")
	html := fs.String("html", "", "write an incident packet as a new HTML file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	if fs.NArg() == 2 && fs.Arg(0) == "packet" {
		id, err := st.ResolveRunID(ctx, fs.Arg(1))
		if err != nil {
			return err
		}
		if *html != "" {
			if err := incident.WritePacketHTML(ctx, st, id, *html); err != nil {
				return err
			}
			_, err = fmt.Fprintln(out, *html)
			return err
		}
		packet, err := incident.BuildPacket(ctx, st, id)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(packet)
	}
	if fs.NArg() != 0 {
		return errors.New("usage: capybara incidents [-project name] [-limit 1-100] [packet <run>]")
	}
	groups, err := incident.List(ctx, st, *project, *limit)
	if err != nil {
		return err
	}
	for _, g := range groups {
		if _, err := fmt.Fprintf(out, "%s  %3d  %-12s  %s  %s\n", g.Fingerprint, g.Count,
			g.Project, g.FirstError, short8(g.Runs[0])); err != nil {
			return err
		}
	}
	return nil
}
