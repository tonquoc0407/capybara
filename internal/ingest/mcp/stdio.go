package mcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/store"
)

// RunStdio executes an MCP server subprocess and intercepts its stdio JSON-RPC traffic.
func RunStdio(ctx context.Context, st *store.Store, opts Options, cmdArgs []string) error {
	if len(cmdArgs) == 0 {
		return fmt.Errorf("no command specified")
	}
	runID := opts.RunID
	if runID == "" {
		runID = os.Getenv("CAPYBARA_RUN_ID")
	}
	if runID == "" {
		runID = "mcp-" + randomHex(8)
	}
	rootSpanID := "root-" + randomHex(8)
	name := opts.Name
	if name == "" {
		name = "mcp: " + filepath.Base(cmdArgs[0])
	}

	rootSpan := store.Span{
		ID:        rootSpanID,
		RunID:     runID,
		Kind:      store.KindAgent,
		Name:      name,
		StartedAt: time.Now().UTC(),
		Status:    "running",
		Attrs: store.Attrs{
			MCP: true,
			Raw: map[string]any{
				"command": strings.Join(cmdArgs, " "),
			},
		},
	}
	_ = st.WriteBatch(ctx, store.Batch{
		Source: "mcp",
		Spans:  []store.Span{rootSpan},
	})

	cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
	cmd.Stderr = os.Stderr

	serverIn, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("mcp stdin pipe: %w", err)
	}
	serverOut, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("mcp stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start mcp server %s: %w", cmdArgs[0], err)
	}

	proxy := NewStreamProxy(st, runID, rootSpanID, opts.Capture)
	proxyErr := proxy.ProxyStreams(ctx, os.Stdin, os.Stdout, serverIn, serverOut)

	waitErr := cmd.Wait()

	rootSpan.EndedAt = time.Now().UTC()
	if waitErr != nil || proxyErr != nil {
		rootSpan.Status = "error"
	} else {
		rootSpan.Status = "ok"
	}
	_ = st.WriteBatch(ctx, store.Batch{
		Source: "mcp",
		Spans:  []store.Span{rootSpan},
	})

	an, err := analyze.New(st)
	if err == nil {
		_ = an.Sweep(ctx)
	}

	if waitErr != nil {
		return waitErr
	}
	return proxyErr
}
