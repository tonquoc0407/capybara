package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

func TestExperimentRunExecutesDatasetAndScoresAnswers(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "eval.db")
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	dataset, err := st.CreateDataset(ctx, "eval-project", "cases")
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"one", "two"} {
		id := "source-" + want
		span := store.Span{ID: id + "-span", RunID: id, Kind: store.KindAgent, Name: "source", StartedAt: time.Now(), EndedAt: time.Now(), Status: "ok"}
		if err := st.WriteBatch(ctx, store.Batch{Source: "test", Spans: []store.Span{span},
			RunContexts: map[string]store.RunContext{id: {Project: dataset.Project}},
			Contents:    []store.Content{{SpanID: span.ID, Role: "user", Body: want}, {SpanID: span.ID, Role: "assistant", Seq: 1, Body: []string{"broken", "wrong"}[i]}}}); err != nil {
			t.Fatal(err)
		}
		item, err := st.AddDatasetItem(ctx, dataset.ID, id)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if _, err := st.SetDatasetExpected(ctx, dataset.ID, item.ID, "one"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	agent := filepath.Join(dir, "agent.py")
	if err := os.WriteFile(agent, []byte("import json,sys\nr=json.load(sys.stdin)\nprint(json.dumps({'assistant':r['messages'][-1]['content'],'cost_usd':0.01}))\n"), 0600); err != nil {
		t.Fatal(err)
	}
	spec := filepath.Join(dir, "spec.json")
	specJSON, _ := json.Marshal(map[string]any{"command": []string{"python3", agent}, "max_cost_usd": 0.1, "turns": []any{map[string]string{"user": "placeholder"}}})
	if err := os.WriteFile(spec, specJSON, 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run(ctx, []string{"-db", db, "experiment", "run", "-name", "candidate", dataset.ID, spec}, &out); err != nil {
		t.Fatal(err)
	}
	var got struct {
		ExperimentID string `json:"experiment_id"`
		Cases        []struct {
			Score float64 `json:"score"`
			RunID string  `json:"run_id"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Cases) != 2 || got.Cases[0].Score != 1 || got.Cases[1].Score != 0 {
		t.Fatalf("result: %s", out.String())
	}
	st, err = store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, c := range got.Cases {
		r, err := st.GetRun(ctx, c.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if r.Project != dataset.Project || r.Environment != "evaluation" || r.CostUSD == nil || *r.CostUSD != 0.01 {
			t.Fatalf("run: %+v", r)
		}
	}
	if _, err := st.CompareExperiments(ctx, got.ExperimentID, got.ExperimentID, "exact_match"); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run(ctx, []string{"-db", db, "-no-content", "experiment", "run", "-name", "private", dataset.ID, spec}, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "one") {
		t.Fatalf("unexpected answer in summary: %s", out.String())
	}
	var private struct {
		ExperimentID string `json:"experiment_id"`
		Cases        []struct {
			RunID string `json:"run_id"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(out.Bytes(), &private); err != nil {
		t.Fatal(err)
	}
	for _, c := range private.Cases {
		body, err := st.Contents(ctx, c.RunID+"-agent")
		if err != nil || len(body) != 0 {
			t.Fatalf("private content: %v, %v", body, err)
		}
	}
	out.Reset()
	if err := run(ctx, []string{"-db", db, "experiment", "compare", "-score", "exact_match", "-min-delta", "0", got.ExperimentID, private.ExperimentID}, &out); err != nil {
		t.Fatalf("compare: %v: %s", err, out.String())
	}
	badAgent := filepath.Join(dir, "bad.py")
	if err := os.WriteFile(badAgent, []byte("import json\nprint(json.dumps({'assistant':'bad','cost_usd':0.0}))\n"), 0600); err != nil {
		t.Fatal(err)
	}
	badSpec := filepath.Join(dir, "bad.json")
	badJSON, _ := json.Marshal(map[string]any{"command": []string{"python3", badAgent}, "max_cost_usd": 0.1, "turns": []any{map[string]string{"user": "placeholder"}}})
	if err := os.WriteFile(badSpec, badJSON, 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run(ctx, []string{"-db", db, "experiment", "run", "-name", "regressed", dataset.ID, badSpec}, &out); err != nil {
		t.Fatal(err)
	}
	var bad struct {
		ExperimentID string `json:"experiment_id"`
		Cases        []struct {
			RunID string  `json:"run_id"`
			Score float64 `json:"score"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(out.Bytes(), &bad); err != nil {
		t.Fatal(err)
	}
	for _, c := range bad.Cases {
		if c.Score != 0 {
			t.Fatalf("bad agent scored %v", c.Score)
		}
		r, err := st.GetRun(ctx, c.RunID)
		if err != nil || r.CostUSD == nil || *r.CostUSD != 0 {
			t.Fatalf("zero reported cost: %+v, %v", r, err)
		}
	}
	out.Reset()
	if err := run(ctx, []string{"-db", db, "experiment", "compare", "-score", "exact_match", "-min-delta", "0", got.ExperimentID, bad.ExperimentID}, &out); err != errBelowThreshold {
		t.Fatalf("expected regression gate, got %v: %s", err, out.String())
	}
}
