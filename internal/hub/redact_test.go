package hub

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/tonquoc0407/capybara/internal/store"
)

func TestRedactBeforeStoragePreservesStructuredAttributes(t *testing.T) {
	s := Server{RedactPattern: regexp.MustCompile(`secret-[0-9]+`)}
	b := store.Batch{
		Spans: []store.Span{{Name: "call secret-42", Attrs: store.Attrs{Raw: map[string]any{
			"nested": []any{map[string]any{"token": "secret-42"}},
		}}}},
		Contents:    []store.Content{{Body: `{"token":"secret-42"}`}},
		RunContexts: map[string]store.RunContext{"r": {UserID: "secret-42"}},
	}
	s.redactBatch(&b)
	encoded, err := json.Marshal(b.Spans[0].Attrs.Raw)
	if err != nil || strings.Contains(string(encoded), "secret-42") || !strings.Contains(string(encoded), "[REDACTED]") {
		t.Fatalf("attributes were not safely redacted: %s %v", encoded, err)
	}
	if strings.Contains(b.Contents[0].Body, "secret-42") || b.RunContexts["r"].UserID != "[REDACTED]" {
		t.Fatal("content or context leaked")
	}
	logs := []store.TraceLog{{Body: "secret-42", AttrsJSON: `{"nested":{"token":"secret-42"}}`}}
	s.redactLogs(logs)
	if strings.Contains(logs[0].Body+logs[0].AttrsJSON, "secret-42") || !json.Valid([]byte(logs[0].AttrsJSON)) {
		t.Fatal("log redaction leaked or corrupted JSON")
	}
}
