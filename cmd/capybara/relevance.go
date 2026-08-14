package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/judge"
	"github.com/tonquoc0407/capybara/internal/store"
)

// relevanceCmd grades whether a run's final answer addresses its initial
// request with an external llm judge. Opt-in and off by default, same shape
// as faithfulness and toolcheck: nothing runs, and nothing leaves the box,
// until an endpoint is configured.
func relevanceCmd(ctx context.Context, dbPath string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("relevance", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	url := fs.String("url", os.Getenv("CAPYBARA_JUDGE_URL"), "OpenAI-compatible api base (e.g. http://localhost:11434/v1)")
	model := fs.String("model", os.Getenv("CAPYBARA_JUDGE_MODEL"), "judge model name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return errors.New("usage: capybara relevance [--url base --model name] [run]")
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
	findings, err := judgeRelevance(ctx, st, runs, client)
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

func judgeRelevance(ctx context.Context, st *store.Store, runs []string, c judge.Completer) ([]store.Finding, error) {
	var findings []store.Finding
	for _, runID := range runs {
		request, answer, span, err := relevanceContext(ctx, st, runID)
		if err != nil {
			return nil, err
		}
		if request == "" || answer == "" {
			continue
		}
		reason, err := judge.GradeRelevance(ctx, c, request, answer)
		if err != nil {
			return nil, fmt.Errorf("relevance run %s: %w", short8(runID), err)
		}
		if reason == "" {
			continue
		}
		detail, err := json.Marshal(map[string]any{"evidence": reason})
		if err != nil {
			return nil, err
		}
		findings = append(findings, store.Finding{
			RunID:    runID,
			SpanID:   span,
			Type:     "off_topic",
			Severity: "note",
			Detail:   string(detail),
		})
	}
	return findings, nil
}

// relevanceContext returns a run's initial user request and its final answer,
// with the span id that produced the answer.
func relevanceContext(ctx context.Context, st *store.Store, runID string) (string, string, string, error) {
	spans, err := st.Spans(ctx, runID)
	if err != nil {
		return "", "", "", err
	}
	var request, answer, answerSpan string
	var answerEnd time.Time
	for _, sp := range spans {
		if sp.Kind != store.KindLLM {
			continue
		}
		cs, err := st.Contents(ctx, sp.ID)
		if err != nil {
			return "", "", "", err
		}
		if request == "" {
			for _, c := range cs {
				if c.Role == "user" {
					request = c.Body
					break
				}
			}
		}
		var b strings.Builder
		for _, c := range cs {
			if c.Role == "assistant" {
				b.WriteString(c.Body)
				b.WriteByte('\n')
			}
		}
		if strings.TrimSpace(b.String()) != "" && !sp.EndedAt.Before(answerEnd) {
			answer, answerSpan, answerEnd = b.String(), sp.ID, sp.EndedAt
		}
	}
	return request, answer, answerSpan, nil
}
