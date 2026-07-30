package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/judge"
	"github.com/tonquoc0407/capybara/internal/store"
)

// judgeClient builds a judge client from configuration, erroring when none is
// set so no command ever sends data to an endpoint the user did not name.
func judgeClient(url, model string) (*judge.Client, error) {
	if url == "" || model == "" {
		return nil, errors.New("no judge configured: set --url and --model (or CAPYBARA_JUDGE_URL/MODEL)")
	}
	return &judge.Client{BaseURL: url, Model: model, Key: os.Getenv("CAPYBARA_JUDGE_KEY")}, nil
}

// toolcheckCmd grades an agent's tool selection with an opt-in llm judge, one
// call per run. Like faithfulness it is off by default and sends the request
// and tool calls out only when an endpoint is configured; its findings are
// advisory notes.
func toolcheckCmd(ctx context.Context, dbPath string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("toolcheck", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	url := fs.String("url", os.Getenv("CAPYBARA_JUDGE_URL"), "OpenAI-compatible api base (e.g. http://localhost:11434/v1)")
	model := fs.String("model", os.Getenv("CAPYBARA_JUDGE_MODEL"), "judge model name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return errors.New("usage: capybara toolcheck [--url base --model name] [run]")
	}
	client, err := judgeClient(*url, *model)
	if err != nil {
		return err
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	runs, err := targetRuns(ctx, st, fs.Args())
	if err != nil {
		return err
	}
	findings, err := judgeTools(ctx, st, runs, client)
	if err != nil {
		return err
	}
	if len(findings) > 0 {
		if err := st.WriteBatch(ctx, store.Batch{Source: "judge", Findings: findings}); err != nil {
			return err
		}
	}
	w := &errWriter{w: out}
	for _, f := range findings {
		w.printf("%-8s %s\n", short8(f.RunID), analyze.FindingLine(f))
	}
	return w.err
}

func judgeTools(ctx context.Context, st *store.Store, runs []string, c judge.Completer) ([]store.Finding, error) {
	var findings []store.Finding
	for _, runID := range runs {
		request, calls, spanIDs, err := toolContext(ctx, st, runID)
		if err != nil {
			return nil, err
		}
		if request == "" || len(calls) == 0 {
			continue
		}
		wrong, err := judge.GradeTools(ctx, c, request, calls)
		if err != nil {
			return nil, fmt.Errorf("toolcheck run %s: %w", short8(runID), err)
		}
		for _, idx := range wrong {
			if idx < 1 || idx > len(calls) {
				continue
			}
			detail, err := json.Marshal(map[string]any{"evidence": calls[idx-1].Name})
			if err != nil {
				return nil, err
			}
			findings = append(findings, store.Finding{
				RunID:    runID,
				SpanID:   spanIDs[idx-1],
				Type:     "wrong_tool",
				Severity: "note",
				Detail:   string(detail),
			})
		}
	}
	return findings, nil
}

// toolContext returns the run's initial user request, the tool calls it made in
// order, and the span id of each call.
func toolContext(ctx context.Context, st *store.Store, runID string) (string, []judge.ToolCall, []string, error) {
	spans, err := st.Spans(ctx, runID)
	if err != nil {
		return "", nil, nil, err
	}
	var request string
	var calls []judge.ToolCall
	var spanIDs []string
	for _, sp := range spans {
		cs, err := st.Contents(ctx, sp.ID)
		if err != nil {
			return "", nil, nil, err
		}
		switch sp.Kind {
		case store.KindLLM:
			if request == "" {
				for _, c := range cs {
					if c.Role == "user" {
						request = c.Body
						break
					}
				}
			}
		case store.KindTool:
			name := sp.Attrs.ToolName
			if name == "" {
				name = sp.Name
			}
			var args string
			for _, c := range cs {
				if c.Role == "input" {
					args = c.Body
					break
				}
			}
			calls = append(calls, judge.ToolCall{Name: name, Args: args})
			spanIDs = append(spanIDs, sp.ID)
		}
	}
	return request, calls, spanIDs, nil
}
