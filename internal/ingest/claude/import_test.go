package claude

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonquoc0407/capybara/internal/store"
)

func TestImportOldClaudeSessionWithoutContent(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	// The fixture's timestamps are months old; explicit import still reads it.
	if err := ImportJSONL(context.Background(), st,
		strings.NewReader(strings.TrimSuffix(session, "\n")),
		"old-session.jsonl", false); err != nil {
		t.Fatal(err)
	}
	runs, err := st.ListRuns(context.Background())
	if err != nil || len(runs) != 1 || runs[0].ID != "old-session" || runs[0].Label != "" {
		t.Fatalf("runs = %+v, %v", runs, err)
	}
	spans, err := st.Spans(context.Background(), "old-session")
	if err != nil || len(spans) == 0 {
		t.Fatalf("spans = %+v, %v", spans, err)
	}
	for _, sp := range spans {
		if sp.Attrs.Raw["cwd"] != nil {
			t.Fatalf("cwd retained under no-content: %+v", sp.Attrs.Raw)
		}
	}
	for _, sp := range spans {
		contents, err := st.Contents(context.Background(), sp.ID)
		if err != nil || len(contents) != 0 {
			t.Fatalf("content for %s = %+v, %v", sp.ID, contents, err)
		}
	}
}
