package export

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/tonquoc0407/capybara/internal/store"
)

// PromptfooCases exports the documented JSON test-case shape. A caller adds
// the resulting file to its own promptfooconfig with tests: file://cases.json.
func PromptfooCases(ctx context.Context, st *store.Store, datasetID string) ([]byte, error) {
	datasets, err := st.ListDatasets(ctx, "")
	if err != nil {
		return nil, err
	}
	found := false
	for _, d := range datasets {
		if d.ID == datasetID {
			found = true
			break
		}
	}
	if !found {
		return nil, errors.New("dataset not found")
	}
	items, err := st.ListDatasetItems(ctx, datasetID)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("dataset has no cases")
	}
	type assertion struct {
		Type  string `json:"type"`
		Value string `json:"value"`
	}
	type testCase struct {
		Description string            `json:"description"`
		Vars        map[string]string `json:"vars"`
		Assert      []assertion       `json:"assert"`
		Metadata    map[string]string `json:"metadata"`
	}
	cases := make([]testCase, 0, len(items))
	for _, item := range items {
		var input, expected string
		if err := json.Unmarshal([]byte(item.InputJSON), &input); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(item.ExpectedJSON), &expected); err != nil {
			return nil, err
		}
		cases = append(cases, testCase{
			Description: "capybara case " + item.ID,
			Vars:        map[string]string{"input": input}, Assert: []assertion{{Type: "equals", Value: expected}},
			Metadata: map[string]string{"source_run_id": item.SourceRunID},
		})
	}
	return json.MarshalIndent(cases, "", "  ")
}

// WritePromptfooCases exports a dataset as Promptfoo exact-answer cases.
func WritePromptfooCases(ctx context.Context, st *store.Store, datasetID, path string) error {
	raw, err := PromptfooCases(ctx, st, datasetID)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(raw, '\n')); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return err
	}
	return nil
}
