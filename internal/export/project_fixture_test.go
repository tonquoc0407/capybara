package export

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonquoc0407/capybara/internal/store"
)

func TestFixtureDoesNotUseAnotherProjectsToolSchema(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "scoped.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sp := store.Span{ID: "beta-tool", RunID: "beta-run", Kind: store.KindTool, Name: "fetch", Status: "ok"}
	if err := st.WriteBatch(ctx, store.Batch{Source: "test", Spans: []store.Span{sp},
		RunContexts: map[string]store.RunContext{sp.RunID: {Project: "beta"}}}); err != nil {
		t.Fatal(err)
	}
	for _, schema := range []store.ToolSchema{
		{Project: "default", ToolName: "fetch", Version: 1, Schema: `{"properties":{"default_private":{"type":"string"}}}`},
		{Project: "beta", ToolName: "fetch", Version: 1, Schema: `{"properties":{"beta_field":{"type":"number"}}}`},
	} {
		if err := st.InsertToolSchema(ctx, schema); err != nil {
			t.Fatal(err)
		}
	}
	got := string(effectiveSchema(ctx, st, sp, "fetch", `{"actual":1}`))
	if strings.Contains(got, "default_private") || !strings.Contains(got, "beta_field") {
		t.Fatalf("foreign fixture schema: %s", got)
	}
}
