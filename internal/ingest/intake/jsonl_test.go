package intake

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonquoc0407/capybara/internal/store"
)

const golden = `{"run":"r1","span":"root","kind":"agent","name":"agent_loop","start":"2026-07-22T10:00:00Z","end":"2026-07-22T10:00:03Z"}

{"run":"r1","span":"llm1","parent":"root","kind":"llm","name":"chat","start":"2026-07-22T10:00:01Z","end":"2026-07-22T10:00:02Z","tokens_in":100,"tokens_out":20,"model":"fake-gpt","contents":[{"role":"user","body":"hi"},{"role":"assistant","body":"{\"a\":1}"}]}
{"run":"r1","span":"tool1","parent":"root","kind":"martian","name":"probe","status":"error"}
`

func openTemp(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestImportJSONL(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	if err := ImportJSONL(ctx, st, strings.NewReader(golden), true); err != nil {
		t.Fatalf("ImportJSONL: %v", err)
	}
	runs, err := st.ListRuns(ctx)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	// The failed probe span is a child: the run's status stays that of its root.
	if len(runs) != 1 || runs[0].Source != "import" || runs[0].Status != "ok" {
		t.Fatalf("runs = %+v", runs)
	}
	if runs[0].TokensIn != 100 || runs[0].ModelMain != "fake-gpt" {
		t.Errorf("aggregates = %+v", runs[0])
	}
	spans, err := st.Spans(ctx, "r1")
	if err != nil {
		t.Fatalf("Spans: %v", err)
	}
	if len(spans) != 3 {
		t.Fatalf("got %d spans, want 3", len(spans))
	}
	for _, sp := range spans {
		if sp.ID == "tool1" && sp.Kind != store.KindOther {
			t.Errorf("unknown kind mapped to %q, want other", sp.Kind)
		}
	}
	contents, err := st.Contents(ctx, "llm1")
	if err != nil {
		t.Fatalf("Contents: %v", err)
	}
	if len(contents) != 2 || contents[1].MediaType != "application/json" {
		t.Errorf("contents = %+v", contents)
	}
}

func TestImportJSONLWithoutContent(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	if err := ImportJSONL(ctx, st, strings.NewReader(golden), false); err != nil {
		t.Fatalf("ImportJSONL: %v", err)
	}
	contents, err := st.Contents(ctx, "llm1")
	if err != nil {
		t.Fatalf("Contents: %v", err)
	}
	if len(contents) != 0 {
		t.Errorf("got %d contents with capture off, want 0", len(contents))
	}
}

func TestImportJSONLReportsBadLine(t *testing.T) {
	st := openTemp(t)
	in := `{"run":"r1","span":"a","kind":"llm","name":"chat"}
not json
`
	err := ImportJSONL(context.Background(), st, strings.NewReader(in), true)
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("err = %v, want line 2 error", err)
	}
	runs, listErr := st.ListRuns(context.Background())
	if listErr != nil {
		t.Fatalf("ListRuns: %v", listErr)
	}
	if len(runs) != 0 {
		t.Errorf("bad file left %d runs behind, want 0", len(runs))
	}
}
