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

// faithfulnessCmd grades retrieval runs with an external llm judge. It is opt-in
// and off by default: without an endpoint it does nothing, and when configured
// it sends each answer and its retrieved documents to that endpoint, so the
// user chooses the model and where the data goes. Findings it writes are notes,
// advisory and llm-sourced, distinct from the deterministic detectors.
func faithfulnessCmd(ctx context.Context, dbPath string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("faithfulness", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	url := fs.String("url", os.Getenv("CAPYBARA_JUDGE_URL"), "OpenAI-compatible api base (e.g. http://localhost:11434/v1)")
	model := fs.String("model", os.Getenv("CAPYBARA_JUDGE_MODEL"), "judge model name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return errors.New("usage: capybara faithfulness [--url base --model name] [run]")
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
	findings, err := judgeRuns(ctx, st, runs, client)
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

func targetRuns(ctx context.Context, st *store.Store, args []string) ([]string, error) {
	if len(args) == 1 {
		run, err := st.ResolveRunID(ctx, args[0])
		if err != nil {
			return nil, err
		}
		return []string{run}, nil
	}
	rs, err := st.ListRuns(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rs))
	for _, r := range rs {
		ids = append(ids, r.ID)
	}
	return ids, nil
}

func judgeRuns(ctx context.Context, st *store.Store, runs []string, c judge.Completer) ([]store.Finding, error) {
	var findings []store.Finding
	for _, runID := range runs {
		answer, span, docs, err := ragContext(ctx, st, runID)
		if err != nil {
			return nil, err
		}
		if answer == "" || len(docs) == 0 {
			continue
		}
		claims, err := judge.Grade(ctx, c, answer, docs)
		if err != nil {
			return nil, fmt.Errorf("judge run %s: %w", short8(runID), err)
		}
		if len(claims) == 0 {
			continue
		}
		detail, err := json.Marshal(map[string]any{"evidence": strings.Join(claims, "; ")})
		if err != nil {
			return nil, err
		}
		findings = append(findings, store.Finding{
			RunID:    runID,
			SpanID:   span,
			Type:     "unfaithful",
			Severity: "note",
			Detail:   string(detail),
		})
	}
	return findings, nil
}

// ragContext returns a run's final answer, the span that produced it, and the
// documents its retrievers returned.
func ragContext(ctx context.Context, st *store.Store, runID string) (string, string, []string, error) {
	spans, err := st.Spans(ctx, runID)
	if err != nil {
		return "", "", nil, err
	}
	var docs []string
	var answer, answerSpan string
	var answerEnd time.Time
	for _, sp := range spans {
		cs, err := st.Contents(ctx, sp.ID)
		if err != nil {
			return "", "", nil, err
		}
		switch sp.Kind {
		case store.KindRetrieval:
			for _, c := range cs {
				if c.Role == "output" {
					docs = append(docs, c.Body)
				}
			}
		case store.KindLLM:
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
	}
	return answer, answerSpan, docs, nil
}
