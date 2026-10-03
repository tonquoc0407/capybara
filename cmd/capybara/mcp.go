package main

import (
	"context"
	"errors"
	"flag"
	"io"

	"github.com/tonquoc0407/capybara/internal/ingest/mcp"
	"github.com/tonquoc0407/capybara/internal/store"
)

func mcpCmd(ctx context.Context, dbPath string, capture bool, args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	runID := fs.String("run", "", "run id to record under")
	name := fs.String("name", "", "name or label for this MCP server")
	sseURL := fs.String("sse", "", "connect to remote MCP SSE server URL instead of local command")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cmdArgs := fs.Args()
	if *sseURL == "" && len(cmdArgs) == 0 {
		return errors.New("usage: capybara mcp [--run id] [--name label] [--sse url | -- <command> [args...]]")
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	opts := mcp.Options{
		RunID:   *runID,
		Name:    *name,
		Capture: capture,
	}

	if *sseURL != "" {
		return mcp.RunSSEBridge(ctx, st, opts, *sseURL)
	}
	return mcp.RunStdio(ctx, st, opts, cmdArgs)
}
