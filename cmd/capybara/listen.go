package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/ingest/otlp"
	"github.com/tonquoc0407/capybara/internal/store"
)

// listenCmd keeps the local receiver and analyzer running without a terminal UI.
func listenCmd(ctx context.Context, dbPath, addr string, capture bool, args []string, out io.Writer) error {
	if len(args) != 0 {
		return errors.New("usage: capybara [flags] listen")
	}
	if ctx.Err() != nil {
		return nil
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
	if addr != "" {
		rcv.HTTPAddr = addr
	}
	listenErr := rcv.Listen()
	defer rcv.Close()
	base := rcv.HTTPBase()
	if base == "" {
		return fmt.Errorf("HTTP listener unavailable: %w", listenErr)
	}
	if _, err := fmt.Fprintf(out, "events %s/api/v1/events\nOTLP   %s\n", base, base); err != nil {
		return err
	}
	if listenErr != nil {
		if _, err := fmt.Fprintf(out, "warning: %v\n", listenErr); err != nil {
			return err
		}
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	type workerResult struct {
		name string
		err  error
	}
	errc := make(chan workerResult, 2)
	go func() { errc <- workerResult{"receiver", rcv.Run(runCtx)} }()
	go func() { errc <- workerResult{"analyzer", an.Watch(runCtx)} }()
	select {
	case <-ctx.Done():
	case result := <-errc:
		cancel()
		if result.err == nil && ctx.Err() == nil {
			result.err = errors.New("stopped unexpectedly")
		}
		other := <-errc
		if result.err != nil {
			result.err = fmt.Errorf("%s: %w", result.name, result.err)
		}
		return errors.Join(result.err, other.err)
	}
	cancel()
	first, second := <-errc, <-errc
	return errors.Join(first.err, second.err)
}
