package replay

import (
	"context"
	"fmt"

	"github.com/tonquoc0407/capybara/internal/store"
)

// BuildCache fills llm_cache from a run's recorded model calls so a replay can
// serve them without touching the network. Spans whose reply was not recorded
// are skipped: replay must never invent one.
func BuildCache(ctx context.Context, st *store.Store, runID string) (int, error) {
	spans, err := st.Spans(ctx, runID)
	if err != nil {
		return 0, err
	}
	contents, err := st.ContentsForRun(ctx, runID)
	if err != nil {
		return 0, err
	}
	var entries []store.CachedLLM
	for _, sp := range spans {
		if sp.Kind != store.KindLLM {
			continue
		}
		request, response := splitTurn(contents[sp.ID])
		if response == "" {
			continue
		}
		entries = append(entries, store.CachedLLM{
			RunID:       runID,
			SpanID:      sp.ID,
			RequestHash: HashLLMRequest(sp.Attrs.Model, request),
			Response:    response,
		})
	}
	if err := st.PutLLMCache(ctx, entries); err != nil {
		return 0, fmt.Errorf("cache run %s: %w", runID, err)
	}
	return len(entries), nil
}

// splitTurn separates an llm span's prompt from the reply it produced.
func splitTurn(contents []store.Content) ([]Message, string) {
	var request []Message
	response := ""
	for _, c := range contents {
		if c.Role == "assistant" {
			if response == "" {
				response = c.Body
			}
			continue
		}
		request = append(request, Message{Role: c.Role, Body: c.Body})
	}
	return request, response
}
