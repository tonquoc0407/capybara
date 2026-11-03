package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/tonquoc0407/capybara/internal/store"
)

func promptCmd(ctx context.Context, dbPath string, args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: capybara prompt <register|list|show>")
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	switch args[0] {
	case "register":
		fs := flag.NewFlagSet("prompt register", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		project := fs.String("project", "default", "project")
		name := fs.String("name", "", "prompt name")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 1 || *name == "" {
			return errors.New("usage: capybara prompt register -name name [-project name] <file>")
		}
		raw, err := os.ReadFile(fs.Arg(0))
		if err != nil {
			return err
		}
		p, err := st.RegisterPrompt(ctx, *project, *name, string(raw), filepath.Base(fs.Arg(0)))
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "%s  v%d  sha256:%s\n", p.ID, p.Version, p.SHA256)
		return err
	case "list":
		fs := flag.NewFlagSet("prompt list", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		project := fs.String("project", "default", "project")
		name := fs.String("name", "", "prompt name")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("usage: capybara prompt list [-project name] [-name name]")
		}
		versions, err := st.ListPromptVersions(ctx, *project, *name)
		if err != nil {
			return err
		}
		for _, p := range versions {
			if _, err := fmt.Fprintf(out, "%s  %-20s v%d  %s\n", p.ID, p.Name, p.Version, p.SHA256); err != nil {
				return err
			}
		}
		return nil
	case "show":
		if len(args) != 2 {
			return errors.New("usage: capybara prompt show <version-id>")
		}
		p, err := st.PromptVersion(ctx, args[1])
		if err != nil {
			return err
		}
		_, err = io.WriteString(out, p.Content)
		return err
	}
	return errors.New("usage: capybara prompt <register|list|show>")
}
