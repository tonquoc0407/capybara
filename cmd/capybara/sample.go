package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/store"
)

func sampleCmd(ctx context.Context, dbPath string, capture bool, args []string, out io.Writer) error {
	if len(args) != 0 {
		return errors.New("usage: capybara sample")
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	const runID = "capybara-sample-v1"
	start := time.Now().UTC()
	b := store.Batch{Source: "sample", DropContent: !capture,
		RunContexts: map[string]store.RunContext{runID: {Environment: "demo", Release: "sample-v1"}},
		Spans: []store.Span{
			{ID: "capybara-sample-root", RunID: runID, Kind: store.KindAgent, Name: "answer product price",
				StartedAt: start, EndedAt: start.Add(3 * time.Second), Status: "error"},
			{ID: "capybara-sample-tool", RunID: runID, ParentID: "capybara-sample-root", Kind: store.KindTool,
				Name: "lookup_price", StartedAt: start.Add(time.Second), EndedAt: start.Add(2 * time.Second), Status: "error",
				Attrs: store.Attrs{ToolName: "lookup_price"}},
			{ID: "capybara-sample-answer", RunID: runID, ParentID: "capybara-sample-root", Kind: store.KindLLM,
				Name: "answer", StartedAt: start.Add(2 * time.Second), EndedAt: start.Add(3 * time.Second), Status: "ok",
				Attrs: store.Attrs{Model: "sample-model"}},
		},
		Contents: []store.Content{
			{SpanID: "capybara-sample-tool", Role: "input", Body: `{"sku":"ABC"}`, MediaType: "application/json"},
			{SpanID: "capybara-sample-tool", Role: "output", Seq: 1, Body: `{"error":"price service unavailable"}`, MediaType: "application/json"},
			{SpanID: "capybara-sample-answer", Role: "user", Body: "What is the price of ABC?", MediaType: "text/plain"},
			{SpanID: "capybara-sample-answer", Role: "assistant", Seq: 1, Body: "The price is $42.", MediaType: "text/plain"},
		},
	}
	if err := st.WriteBatch(ctx, b); err != nil {
		return err
	}
	if err := st.SetRunLabel(ctx, runID, "sample", "sample: price lookup failed"); err != nil {
		return err
	}
	an, err := analyze.New(st)
	if err != nil {
		return err
	}
	if err := an.Sweep(ctx); err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, runID)
	return err
}
