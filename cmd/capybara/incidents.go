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
	if len(args) > 0 && (args[0] == "annotate" || args[0] == "annotations") {
		return incidentAnnotationCmd(ctx, dbPath, args, out)
	}
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
		return errors.New("usage: capybara incidents [-project name] [-limit 1-100] [packet <run>] | annotate | annotations")
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

func incidentAnnotationCmd(ctx context.Context, dbPath string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("incident annotations", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	status := fs.String("status", "open", "annotation status: open, investigating, resolved or ignored")
	note := fs.String("note", "", "investigation or disposition note")
	author := fs.String("author", "local", "note author")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	if args[0] == "annotations" {
		if fs.NArg() != 1 {
			return errors.New("usage: capybara incidents annotations <run>")
		}
		id, err := st.ResolveRunID(ctx, fs.Arg(0))
		if err != nil {
			return err
		}
		items, err := st.IncidentAnnotations(ctx, id)
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(items)
	}
	if fs.NArg() != 1 {
		return errors.New("usage: capybara incidents annotate [-status open] [-note text] [-author name] <run>")
	}
	id, err := st.ResolveRunID(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	item, err := st.AddIncidentAnnotation(ctx, id, *status, *note, *author)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(item)
}
