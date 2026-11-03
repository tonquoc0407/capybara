package export

import (
	"context"
	"encoding/json"
	"io"

	"github.com/tonquoc0407/capybara/internal/store"
)

// WriteSpanJSONL writes the generic span-per-line format accepted by
// `capybara import`. It deliberately describes spans and their bodies only;
// use a database backup to preserve logs, scores and other linked records.
func WriteSpanJSONL(ctx context.Context, st *store.Store, runID string, out io.Writer) error {
	spans, err := st.Spans(ctx, runID)
	if err != nil {
		return err
	}
	contents, err := st.ContentsForRun(ctx, runID)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(out)
	for _, sp := range spans {
		line := struct {
			Run       string         `json:"run"`
			Span      string         `json:"span"`
			Parent    string         `json:"parent,omitempty"`
			Kind      string         `json:"kind"`
			Name      string         `json:"name"`
			Start     any            `json:"start"`
			End       any            `json:"end"`
			TokensIn  int64          `json:"tokens_in"`
			TokensOut int64          `json:"tokens_out"`
			Status    string         `json:"status"`
			Model     string         `json:"model,omitempty"`
			Provider  string         `json:"provider,omitempty"`
			Tool      string         `json:"tool,omitempty"`
			Attrs     map[string]any `json:"attrs,omitempty"`
			Contents  []struct {
				Role string `json:"role"`
				Body string `json:"body"`
			} `json:"contents,omitempty"`
		}{Run: runID, Span: sp.ID, Parent: sp.ParentID, Kind: string(sp.Kind), Name: sp.Name,
			Start: sp.StartedAt, End: sp.EndedAt, TokensIn: sp.TokensIn, TokensOut: sp.TokensOut,
			Status: sp.Status, Model: sp.Attrs.Model, Provider: sp.Attrs.Provider,
			Tool: sp.Attrs.ToolName, Attrs: sp.Attrs.Raw}
		for _, c := range contents[sp.ID] {
			line.Contents = append(line.Contents, struct {
				Role string `json:"role"`
				Body string `json:"body"`
			}{Role: c.Role, Body: c.Body})
		}
		if err := encoder.Encode(line); err != nil {
			return err
		}
	}
	return nil
}
