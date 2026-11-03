package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/tonquoc0407/capybara/internal/scenario"
	"github.com/tonquoc0407/capybara/internal/store"
)

// experimentRunCmd evaluates each captured case with a fresh scenario process.
// A failed answer is a scored result; setup or persistence errors stop the run.
func experimentRunCmd(ctx context.Context, st *store.Store, capture bool, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("experiment run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	name := fs.String("name", "", "experiment name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 || strings.TrimSpace(*name) == "" {
		return errors.New("usage: capybara experiment run -name <name> <dataset-id> <scenario-spec.json>")
	}
	dataset, err := st.GetDataset(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	items, err := st.ListDatasetItems(ctx, dataset.ID)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return errors.New("dataset is empty")
	}
	raw, err := os.ReadFile(fs.Arg(1))
	if err != nil {
		return err
	}
	var spec scenario.Spec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return fmt.Errorf("scenario JSON: %w", err)
	}
	type testCase struct {
		input, expected string
		item            store.DatasetItem
	}
	cases := make([]testCase, 0, len(items))
	for _, item := range items {
		var input, expected string
		if err := json.Unmarshal([]byte(item.InputJSON), &input); err != nil {
			return fmt.Errorf("item %s input: %w", item.ID, err)
		}
		if err := json.Unmarshal([]byte(item.ExpectedJSON), &expected); err != nil {
			return fmt.Errorf("item %s expected: %w", item.ID, err)
		}
		if input == "" || expected == "" {
			return fmt.Errorf("item %s needs captured input and output", item.ID)
		}
		cases = append(cases, testCase{input, expected, item})
	}
	spec.Turns = []scenario.Turn{{User: cases[0].input}}
	if err := spec.Validate(); err != nil {
		return err
	}
	experiment, err := st.CreateExperiment(ctx, dataset.ID, *name)
	if err != nil {
		return err
	}
	// Creating the experiment freezes the dataset. Verify that preparation
	// did not race an editor before using the earlier input/answer snapshot.
	frozenItems, err := st.ListDatasetItems(ctx, dataset.ID)
	if err != nil {
		return err
	}
	if len(frozenItems) != len(items) {
		return errors.New("dataset changed during preparation; rerun using the frozen dataset")
	}
	for i := range items {
		if items[i] != frozenItems[i] {
			return errors.New("dataset changed during preparation; rerun using the frozen dataset")
		}
	}
	type caseResult struct {
		ItemID string  `json:"item_id"`
		RunID  string  `json:"run_id"`
		Score  float64 `json:"score"`
		Reason string  `json:"reason,omitempty"`
	}
	result := struct {
		ExperimentID string       `json:"experiment_id"`
		ScoreName    string       `json:"score_name"`
		Cases        []caseResult `json:"cases"`
	}{
		ExperimentID: experiment.ID, ScoreName: "exact_match", Cases: make([]caseResult, 0, len(cases)),
	}
	for _, tc := range cases {
		if err := ctx.Err(); err != nil {
			return err
		}
		caseSpec := spec
		caseSpec.Turns = []scenario.Turn{{User: tc.input}}
		started := time.Now().UTC()
		outcome, err := scenario.Run(ctx, caseSpec)
		if err != nil {
			return err
		}
		ended := time.Now().UTC()
		idBytes := make([]byte, 16)
		if _, err := rand.Read(idBytes); err != nil {
			return err
		}
		runID := hex.EncodeToString(idBytes)
		spanID := runID + "-agent"
		answer, reason := "", ""
		if len(outcome.Turns) > 0 {
			answer, reason = outcome.Turns[0].Assistant, outcome.Turns[0].Reason
		}
		score, status := 0.0, "error"
		if outcome.Passed && answer == tc.expected {
			score, status = 1, "ok"
		} else if reason == "" {
			reason = "answer differs from expected output"
		}
		span := store.Span{ID: spanID, RunID: runID, Kind: store.KindAgent, Name: *name,
			StartedAt: started, EndedAt: ended, Status: status}
		if outcome.CostReported {
			span.CostUSD = &outcome.ReportedCostUSD
		}
		batch := store.Batch{Source: "experiment", Spans: []store.Span{span},
			RunContexts: map[string]store.RunContext{runID: {Project: dataset.Project, Environment: "evaluation"}}}
		if capture {
			batch.Contents = []store.Content{
				{SpanID: spanID, Role: "user", Seq: 0, Body: tc.input, MediaType: "text/plain"},
				{SpanID: spanID, Role: "assistant", Seq: 1, Body: answer, MediaType: "text/plain"},
			}
		}
		if err := st.WriteBatch(ctx, batch); err != nil {
			return err
		}
		if outcome.CostReported {
			if err := st.SetSpanEstimates(ctx, map[string]store.SpanEstimate{spanID: {
				CostUSD: outcome.ReportedCostUSD, Provenance: store.CostProvenance{
					Kind: "estimate", PriceSource: "agent_report", PriceVersion: "unknown", UsageSource: "agent_report", CheckedAt: ended,
				},
			}}); err != nil {
				return err
			}
		}
		if _, err := st.AddScore(ctx, runID, tc.item.ID, "exact_match", score, "scenario", reason); err != nil {
			return err
		}
		if err := st.RecordExperimentResult(ctx, experiment.ID, tc.item.ID, runID, "exact_match"); err != nil {
			return err
		}
		result.Cases = append(result.Cases, caseResult{tc.item.ID, runID, score, reason})
	}
	return json.NewEncoder(out).Encode(result)
}
