package analyze

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

var t0 = time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)

func openTemp(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// toolRun writes one run with a single completed tool call and its output.
func toolRun(t *testing.T, st *store.Store, runID, tool, output string, at time.Time) {
	t.Helper()
	sp := store.Span{
		ID: runID + "-t", RunID: runID, Kind: store.KindTool, Name: tool,
		StartedAt: at, EndedAt: at.Add(time.Second), Status: "ok",
		Attrs: store.Attrs{ToolName: tool},
	}
	b := store.Batch{Source: "test", Spans: []store.Span{sp}}
	b.Contents = []store.Content{{
		SpanID: sp.ID, Role: "output", Seq: 0, Body: output, MediaType: "application/json",
	}}
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
}

func newAnalyzer(t *testing.T, st *store.Store) *Analyzer {
	t.Helper()
	a, err := New(st)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

func sweep(t *testing.T, st *store.Store) *Analyzer {
	t.Helper()
	a := newAnalyzer(t, st)
	if err := a.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	return a
}

func runFindings(t *testing.T, st *store.Store, runID string) []store.Finding {
	t.Helper()
	fs, err := st.Findings(context.Background(), runID)
	if err != nil {
		t.Fatalf("Findings: %v", err)
	}
	return fs
}

func TestLearnsThenWidensSilently(t *testing.T) {
	st := openTemp(t)
	toolRun(t, st, "r1", "fetch", `{"price":42}`, t0)
	sweep(t, st)
	toolRun(t, st, "r2", "fetch", `{"price":41,"currency":"USD"}`, t0.Add(time.Minute))
	sweep(t, st)
	if fs := runFindings(t, st, "r2"); len(fs) != 0 {
		t.Fatalf("widening produced findings: %+v", fs)
	}
	ts, err := st.LatestToolSchema(context.Background(), "fetch")
	if err != nil || ts == nil {
		t.Fatalf("LatestToolSchema: %v %v", ts, err)
	}
	if ts.Version != 1 || !strings.Contains(ts.Schema, "currency") {
		t.Errorf("schema = v%d %s", ts.Version, ts.Schema)
	}
}

func TestDriftOnMissingFieldAdoptsNewVersion(t *testing.T) {
	st := openTemp(t)
	toolRun(t, st, "r1", "fetch", `{"price":42,"currency":"USD"}`, t0)
	sweep(t, st)
	toolRun(t, st, "r2", "fetch", `{"price":42}`, t0.Add(time.Minute))
	sweep(t, st)
	fs := runFindings(t, st, "r2")
	if len(fs) != 1 || fs[0].Type != "drift" || !strings.Contains(fs[0].Detail, `"currency"`) {
		t.Fatalf("findings = %+v", fs)
	}
	ts, err := st.LatestToolSchema(context.Background(), "fetch")
	if err != nil || ts.Version != 2 {
		t.Fatalf("schema after drift = %+v, %v", ts, err)
	}
	toolRun(t, st, "r3", "fetch", `{"price":42}`, t0.Add(2*time.Minute))
	sweep(t, st)
	if fs := runFindings(t, st, "r3"); len(fs) != 0 {
		t.Errorf("post-adoption findings = %+v", fs)
	}
}

func TestDriftOnRetypedField(t *testing.T) {
	st := openTemp(t)
	toolRun(t, st, "r1", "fetch", `{"price":42}`, t0)
	sweep(t, st)
	toolRun(t, st, "r2", "fetch", `{"price":"42"}`, t0.Add(time.Minute))
	sweep(t, st)
	fs := runFindings(t, st, "r2")
	if len(fs) != 1 || fs[0].Type != "drift" ||
		!strings.Contains(fs[0].Detail, `"want":"number"`) ||
		!strings.Contains(fs[0].Detail, `"got":"string"`) {
		t.Fatalf("findings = %+v", fs)
	}
}

func TestMalformedWhenContractSaysJSON(t *testing.T) {
	st := openTemp(t)
	toolRun(t, st, "r1", "fetch", `{"price":42}`, t0)
	sweep(t, st)
	toolRun(t, st, "r2", "fetch", `garbage{{not json`, t0.Add(time.Minute))
	sweep(t, st)
	fs := runFindings(t, st, "r2")
	if len(fs) != 1 || fs[0].Type != "malformed" {
		t.Fatalf("findings = %+v", fs)
	}
}

func TestTextToolsNeverDriftOrMalform(t *testing.T) {
	st := openTemp(t)
	toolRun(t, st, "r1", "bash", "ok\ttests pass", t0)
	sweep(t, st)
	toolRun(t, st, "r2", "bash", "compile error on line 3", t0.Add(time.Minute))
	sweep(t, st)
	if fs := runFindings(t, st, "r2"); len(fs) != 0 {
		t.Fatalf("text tool findings = %+v", fs)
	}
}

func TestEmptyPayloadFinding(t *testing.T) {
	st := openTemp(t)
	toolRun(t, st, "r1", "fetch", `  `, t0)
	sweep(t, st)
	fs := runFindings(t, st, "r1")
	if len(fs) != 1 || fs[0].Type != "empty_payload" {
		t.Fatalf("findings = %+v", fs)
	}
}

func TestDeclaredSchemaBeatsLearned(t *testing.T) {
	st := openTemp(t)
	sp := store.Span{
		ID: "d1", RunID: "r1", Kind: store.KindTool, Name: "fetch",
		StartedAt: t0, EndedAt: t0.Add(time.Second), Status: "ok",
		Attrs: store.Attrs{ToolName: "fetch", Raw: map[string]any{
			"capybara.schema": `{"type":"object","properties":{"price":{"type":"number"}},"required":["price"]}`,
		}},
	}
	b := store.Batch{Source: "test", Spans: []store.Span{sp}, Contents: []store.Content{
		{SpanID: "d1", Role: "output", Seq: 0, Body: `{"price":"nope"}`, MediaType: "application/json"},
	}}
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	sweep(t, st)
	fs := runFindings(t, st, "r1")
	if len(fs) != 1 || fs[0].Type != "drift" || !strings.Contains(fs[0].Detail, "number") {
		t.Fatalf("findings = %+v", fs)
	}
	if ts, err := st.LatestToolSchema(context.Background(), "fetch"); err != nil || ts != nil {
		t.Errorf("declared validation must not store learned schema: %+v, %v", ts, err)
	}
}

// improviseRunBatch builds an agent turn: llm1 calls a failing tool, llm2 answers.
func improviseRunBatch(runID, toolOutput, llmAnswer string, toolStatus string) store.Batch {
	spans := []store.Span{
		{
			ID: runID + "-root", RunID: runID, Kind: store.KindAgent, Name: "agent",
			StartedAt: t0, EndedAt: t0.Add(10 * time.Second), Status: "ok",
		},
		{
			ID: runID + "-llm1", RunID: runID, ParentID: runID + "-root", Kind: store.KindLLM,
			Name: "chat", StartedAt: t0, EndedAt: t0.Add(2 * time.Second), Status: "ok",
		},
		{
			ID: runID + "-tool", RunID: runID, ParentID: runID + "-llm1", Kind: store.KindTool,
			Name: "search_db", StartedAt: t0.Add(2 * time.Second),
			EndedAt: t0.Add(3 * time.Second), Status: toolStatus,
			Attrs: store.Attrs{ToolName: "search_db"},
		},
		{
			ID: runID + "-llm2", RunID: runID, ParentID: runID + "-root", Kind: store.KindLLM,
			Name: "chat", StartedAt: t0.Add(4 * time.Second),
			EndedAt: t0.Add(6 * time.Second), Status: "ok",
		},
	}
	contents := []store.Content{
		{SpanID: runID + "-llm2", Role: "assistant", Seq: 0, Body: llmAnswer, MediaType: "text/plain"},
	}
	if toolOutput != "" {
		contents = append(contents, store.Content{
			SpanID: runID + "-tool", Role: "output", Seq: 0, Body: toolOutput, MediaType: "text/plain",
		})
	}
	return store.Batch{Source: "test", Spans: spans, Contents: contents}
}

func TestImprovisedWhenOutputMentionsFailedTool(t *testing.T) {
	st := openTemp(t)
	b := improviseRunBatch("r1", "connection refused", "The search_db results show the price is 42.", "error")
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	sweep(t, st)
	fs := runFindings(t, st, "r1")
	if len(fs) != 1 || fs[0].Type != "improvised" || fs[0].SpanID != "r1-llm2" {
		t.Fatalf("findings = %+v", fs)
	}
	if !strings.Contains(fs[0].Detail, "search_db") || !strings.Contains(fs[0].Detail, `"cause":"error"`) {
		t.Errorf("detail = %s", fs[0].Detail)
	}
}

func TestNotImprovisedWhenFailureAcknowledged(t *testing.T) {
	st := openTemp(t)
	b := improviseRunBatch("r1", "connection refused", "search_db failed, I cannot retrieve the price.", "error")
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	sweep(t, st)
	if fs := runFindings(t, st, "r1"); len(fs) != 0 {
		t.Fatalf("findings = %+v", fs)
	}
}

func TestNotImprovisedWithoutReference(t *testing.T) {
	st := openTemp(t)
	b := improviseRunBatch("r1", "connection refused", "Let me try a different approach instead.", "error")
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	sweep(t, st)
	if fs := runFindings(t, st, "r1"); len(fs) != 0 {
		t.Fatalf("findings = %+v", fs)
	}
}

func TestImprovisedWhenQuotingBrokenOutput(t *testing.T) {
	st := openTemp(t)
	// The lookup tool has a JSON contract from r0, then emits garbage in r1.
	toolRun(t, st, "r0", "lookup", `{"price":42}`, t0.Add(-time.Hour))
	sweep(t, st)
	b := improviseRunBatch("r1", `PRICE=42;CURRENCY=USD garbage`,
		"According to the data, PRICE=42;CURRENCY=USD, so you are set.", "ok")
	b.Spans[2].Name = "lookup"
	b.Spans[2].Attrs.ToolName = "lookup"
	b.Contents[1].MediaType = "text/plain"
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	sweep(t, st)
	fs := runFindings(t, st, "r1")
	types := map[string]string{}
	for _, f := range fs {
		types[f.Type] = f.SpanID
	}
	if types["malformed"] != "r1-tool" {
		t.Fatalf("findings = %+v", fs)
	}
	if types["improvised"] != "r1-llm2" {
		t.Fatalf("findings = %+v", fs)
	}
}

func TestSweepIsIdempotent(t *testing.T) {
	st := openTemp(t)
	b := improviseRunBatch("r1", "boom", "The search_db results show the price is 42.", "error")
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	sweep(t, st)
	first := len(runFindings(t, st, "r1"))
	// Re-ingesting the same spans resets the analyzed flag; findings dedupe.
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	sweep(t, st)
	if got := len(runFindings(t, st, "r1")); got != first {
		t.Fatalf("findings after re-sweep = %d, want %d", got, first)
	}
}

func TestWatchAnalyzesOnWrite(t *testing.T) {
	st := openTemp(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- newAnalyzer(t, st).Watch(ctx) }()
	time.Sleep(50 * time.Millisecond)
	b := improviseRunBatch("r1", "boom", "The search_db results show the price is 42.", "error")
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(runFindings(t, st, "r1")) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(runFindings(t, st, "r1")) == 0 {
		t.Fatal("watch never produced findings")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Watch: %v", err)
	}
}
