package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/tonquoc0407/capybara/internal/export"
	"github.com/tonquoc0407/capybara/internal/store"
)

func datasetCmd(ctx context.Context, dbPath string, args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: capybara dataset create|list|add|items|expect|clone|export ...")
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	switch args[0] {
	case "expect":
		if len(args) != 4 {
			return errors.New("usage: capybara dataset expect <dataset-id> <item-id> <expected-file>")
		}
		raw, err := os.ReadFile(args[3])
		if err != nil {
			return err
		}
		item, err := st.SetDatasetExpected(ctx, args[1], args[2], string(raw))
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(item)
	case "clone":
		if len(args) != 3 {
			return errors.New("usage: capybara dataset clone <dataset-id> <name>")
		}
		d, err := st.CloneDataset(ctx, args[1], args[2])
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, d.ID)
		return err
	case "create":
		fs := flag.NewFlagSet("dataset create", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		project := fs.String("project", "default", "project")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return errors.New("usage: capybara dataset create [-project name] <name>")
		}
		d, err := st.CreateDataset(ctx, *project, fs.Arg(0))
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "%s\n", d.ID)
		return err
	case "list":
		fs := flag.NewFlagSet("dataset list", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		project := fs.String("project", "", "project")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("usage: capybara dataset list [-project name]")
		}
		datasets, err := st.ListDatasets(ctx, *project)
		if err != nil {
			return err
		}
		for _, d := range datasets {
			if _, err := fmt.Fprintf(out, "%s  %s  %s\n", d.ID, d.Project, d.Name); err != nil {
				return err
			}
		}
		return nil
	case "add":
		if len(args) != 3 {
			return errors.New("usage: capybara dataset add <dataset-id> <run>")
		}
		run, err := st.ResolveRunID(ctx, args[2])
		if err != nil {
			return err
		}
		item, err := st.AddDatasetItem(ctx, args[1], run)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, item.ID)
		return err
	case "items":
		if len(args) != 2 {
			return errors.New("usage: capybara dataset items <dataset-id>")
		}
		items, err := st.ListDatasetItems(ctx, args[1])
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(items)
	case "export":
		if len(args) != 4 || args[1] != "promptfoo" {
			return errors.New("usage: capybara dataset export promptfoo <dataset-id> <cases.json>")
		}
		if err := export.WritePromptfooCases(ctx, st, args[2], args[3]); err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, args[3])
		return err
	}
	return errors.New("usage: capybara dataset create|list|add|items|expect|clone|export ...")
}

func scoreCmd(ctx context.Context, dbPath string, args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "add" {
		return errors.New("usage: capybara score add -name metric -value 0..1 [-item id] <run>")
	}
	fs := flag.NewFlagSet("score add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	name := fs.String("name", "", "score name")
	value := fs.Float64("value", math.NaN(), "normalized score")
	item := fs.String("item", "", "dataset item id")
	source := fs.String("source", "human", "score source")
	comment := fs.String("comment", "", "feedback")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: capybara score add -name metric -value 0..1 [-item id] <run>")
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
	sc, err := st.AddScore(ctx, run, *item, *name, *value, *source, *comment)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, sc.ID)
	return err
}

func experimentCmd(ctx context.Context, dbPath string, capture bool, args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: capybara experiment create|record|compare ...")
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	switch args[0] {
	case "run":
		return experimentRunCmd(ctx, st, capture, args[1:], out)
	case "create":
		if len(args) != 3 {
			return errors.New("usage: capybara experiment create <dataset-id> <name>")
		}
		e, err := st.CreateExperiment(ctx, args[1], args[2])
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, e.ID)
		return err
	case "record":
		if len(args) != 5 {
			return errors.New("usage: capybara experiment record <experiment-id> <item-id> <run> <score-name>")
		}
		run, err := st.ResolveRunID(ctx, args[3])
		if err != nil {
			return err
		}
		return st.RecordExperimentResult(ctx, args[1], args[2], run, args[4])
	case "compare":
		fs := flag.NewFlagSet("experiment compare", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		score := fs.String("score", "", "score name")
		minDelta := fs.Float64("min-delta", math.Inf(-1), "minimum acceptable candidate minus baseline")
		junit := fs.String("junit", "", "write a JUnit XML result to this new file")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 2 || *score == "" {
			return errors.New("usage: capybara experiment compare -score metric [-min-delta value] <baseline-id> <candidate-id>")
		}
		comparison, err := st.CompareExperiments(ctx, fs.Arg(0), fs.Arg(1), *score)
		if err != nil {
			return err
		}
		if err := json.NewEncoder(out).Encode(comparison); err != nil {
			return err
		}
		if *junit != "" {
			if err := export.WriteExperimentJUnit(*junit, comparison, *minDelta); err != nil {
				return err
			}
		}
		if comparison.Delta < *minDelta {
			return errBelowThreshold
		}
		return nil
	}
	return errors.New("usage: capybara experiment create|record|compare ...")
}
