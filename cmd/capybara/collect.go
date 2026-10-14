package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/ingest/claude"
	"github.com/tonquoc0407/capybara/internal/ingest/otlp"
	"github.com/tonquoc0407/capybara/internal/store"
)

// collectCmd runs the receiver and analyzer without starting a terminal UI.
// Unlike the interactive mode, it never reads Claude sessions implicitly.
func collectCmd(ctx context.Context, dbPath, otlpAddr string, capture bool, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("collect", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	grpcAddr := fs.String("grpc", otlp.DefaultGRPCAddr, "OTLP gRPC address to listen on")
	watchClaude := fs.Bool("watch-claude", false, "also tail Claude Code sessions")
	claudeRoot := fs.String("claude-root", "", "Claude Code projects directory (implies -watch-claude)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: capybara [flags] collect [-grpc host:port] [-watch-claude] [-claude-root path]")
	}
	if otlpAddr == "" {
		otlpAddr = otlp.DefaultHTTPAddr
	}
	root, err := collectClaudeRoot(*watchClaude, *claudeRoot)
	if err != nil {
		return err
	}

	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	an, err := analyze.New(st)
	if err != nil {
		return err
	}
	rcv := otlp.New(st, capture)
	rcv.GRPCAddr, rcv.HTTPAddr = *grpcAddr, otlpAddr
	listenErr := rcv.Listen()
	defer rcv.Close()
	if !rcv.Listening() {
		if listenErr != nil {
			return listenErr
		}
		return errors.New("no OTLP transport could listen")
	}
	if err := writeCollectStatus(out, dbPath, capture, root, rcv, listenErr); err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	workers := []func() error{
		func() error { return rcv.Run(runCtx) },
		func() error { return an.Watch(runCtx) },
	}
	if root != "" {
		workers = append(workers, func() error { return claude.Watch(runCtx, st, root, capture) })
	}
	errCh := make(chan error, len(workers))
	for _, worker := range workers {
		go func() { errCh <- worker() }()
	}
	errs := []error{<-errCh}
	cancel()
	for range len(workers) - 1 {
		errs = append(errs, <-errCh)
	}
	return errors.Join(errs...)
}

func collectClaudeRoot(watch bool, root string) (string, error) {
	if root != "" {
		watch = true
	}
	if !watch {
		return "", nil
	}
	if root == "" {
		root = claude.DefaultRoot()
	}
	if root == "" {
		return "", errors.New("claude Code projects directory not found; use -claude-root path")
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("claude root %s: %w", root, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("claude root %s is not a directory", root)
	}
	return root, nil
}

func writeCollectStatus(out io.Writer, dbPath string, capture bool, claudeRoot string,
	rcv *otlp.Receiver, listenErr error,
) error {
	var status bytes.Buffer
	fmt.Fprintf(&status, "database %s\n", dbPath)
	if addr := rcv.GRPCAddress(); addr != "" {
		fmt.Fprintf(&status, "otlp grpc %s\n", addr)
	} else {
		fmt.Fprintln(&status, "otlp grpc unavailable")
	}
	if endpoint := rcv.HTTPBase(); endpoint != "" {
		fmt.Fprintf(&status, "otlp http %s\n", endpoint)
	} else {
		fmt.Fprintln(&status, "otlp http unavailable")
	}
	if capture {
		fmt.Fprintln(&status, "content recorded")
	} else {
		fmt.Fprintln(&status, "content dropped")
	}
	if claudeRoot != "" {
		fmt.Fprintf(&status, "watching claude %s\n", claudeRoot)
	}
	if listenErr != nil {
		fmt.Fprintf(&status, "warning %v\n", listenErr)
	}
	_, err := io.Copy(out, &status)
	return err
}
