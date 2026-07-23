package export

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

func openTemp(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestBuildFixtureCapturesToolContract(t *testing.T) {
	st := openTemp(t)
	at := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	sp := store.Span{
		ID: "t1", RunID: "r1", Kind: store.KindTool, Name: "fetch",
		StartedAt: at, EndedAt: at.Add(time.Second), Status: "ok",
		Attrs: store.Attrs{ToolName: "fetch"},
	}
	b := store.Batch{Source: "test", Spans: []store.Span{sp}, Contents: []store.Content{
		{SpanID: "t1", Role: "input", Seq: 0, Body: `{"sku":"A"}`, MediaType: "application/json"},
		{SpanID: "t1", Role: "output", Seq: 1, Body: `{"price":42}`, MediaType: "application/json"},
	}}
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	fx, err := BuildFixture(context.Background(), st, "r1")
	if err != nil {
		t.Fatalf("BuildFixture: %v", err)
	}
	if len(fx.Tools) != 1 {
		t.Fatalf("tools = %d, want 1", len(fx.Tools))
	}
	tf := fx.Tools[0]
	if tf.Tool != "fetch" || tf.Input != `{"sku":"A"}` || tf.Output != `{"price":42}` || tf.Hash == "" {
		t.Errorf("tool fixture = %+v", tf)
	}
	if !strings.Contains(string(tf.Schema), "price") {
		t.Errorf("schema missing price: %s", tf.Schema)
	}
	if tf.Target != "" {
		t.Errorf("target = %q, want none for a run the sdk did not trace", tf.Target)
	}
}

func TestBuildFixtureCarriesTheToolTarget(t *testing.T) {
	st := openTemp(t)
	at := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	sp := store.Span{
		ID: "t1", RunID: "r1", Kind: store.KindTool, Name: "fetch",
		StartedAt: at, EndedAt: at.Add(time.Second), Status: "ok",
		Attrs: store.Attrs{
			ToolName: "fetch",
			Raw:      map[string]any{"capybara.target": "catalogue:fetch_price"},
		},
	}
	b := store.Batch{Source: "test", Spans: []store.Span{sp}, Contents: []store.Content{
		{SpanID: "t1", Role: "input", Seq: 0, Body: `{"sku":"A"}`, MediaType: "application/json"},
		{SpanID: "t1", Role: "output", Seq: 1, Body: `{"price":42}`, MediaType: "application/json"},
	}}
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	fx, err := BuildFixture(context.Background(), st, "r1")
	if err != nil {
		t.Fatalf("BuildFixture: %v", err)
	}
	if fx.Tools[0].Target != "catalogue:fetch_price" {
		t.Errorf("target = %q", fx.Tools[0].Target)
	}
}

func TestBuildSpanFixtureNarrowsToOneCall(t *testing.T) {
	st := openTemp(t)
	at := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	spans := []store.Span{
		{
			ID: "t1", RunID: "r1", Kind: store.KindTool, Name: "fetch",
			StartedAt: at, EndedAt: at.Add(time.Second), Status: "ok",
			Attrs: store.Attrs{ToolName: "fetch"},
		},
		{
			ID: "t2", RunID: "r1", Kind: store.KindTool, Name: "lookup",
			StartedAt: at.Add(time.Second), EndedAt: at.Add(2 * time.Second), Status: "ok",
			Attrs: store.Attrs{ToolName: "lookup"},
		},
	}
	b := store.Batch{Source: "test", Spans: spans, Contents: []store.Content{
		{SpanID: "t1", Role: "input", Seq: 0, Body: `{"sku":"A"}`, MediaType: "application/json"},
		{SpanID: "t1", Role: "output", Seq: 1, Body: `{"price":42}`, MediaType: "application/json"},
		{SpanID: "t2", Role: "input", Seq: 0, Body: `{"id":1}`, MediaType: "application/json"},
		{SpanID: "t2", Role: "output", Seq: 1, Body: `{"name":"x"}`, MediaType: "application/json"},
	}}
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	fx, err := BuildSpanFixture(context.Background(), st, "r1", "t2")
	if err != nil {
		t.Fatalf("BuildSpanFixture: %v", err)
	}
	if len(fx.Tools) != 1 || fx.Tools[0].Tool != "lookup" || fx.Span != "t2" {
		t.Fatalf("span fixture = %+v", fx)
	}
	paths, err := WritePytest(t.TempDir(), fx)
	if err != nil {
		t.Fatalf("WritePytest: %v", err)
	}
	if base := filepath.Base(paths[1]); base != "test_r1_t2.py" {
		t.Errorf("span test path = %s, want test_r1_t2.py", base)
	}
	if _, err := BuildSpanFixture(context.Background(), st, "r1", "nope"); err == nil {
		t.Error("BuildSpanFixture accepted a span with no tool call")
	}
}

func TestWritePytestEmitsRunnableModule(t *testing.T) {
	fx := Fixture{Run: "abcdef1234", Source: "otlp", Tools: []ToolFixture{{
		Hash: "h1", SpanID: "t1", Tool: "fetch", Input: `{"sku":"A"}`,
		Output: `{"price":42}`, Schema: json.RawMessage(`{"type":["object"]}`),
	}}}
	dir := t.TempDir()
	paths, err := WritePytest(dir, fx)
	if err != nil {
		t.Fatalf("WritePytest: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("paths = %v, want fixture + test", paths)
	}
	if filepath.Base(paths[0]) != "abcdef12_fixture.json" {
		t.Errorf("fixture path = %s", paths[0])
	}
	raw, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := json.Unmarshal(raw, &Fixture{}); err != nil {
		t.Errorf("fixture is not valid json: %v", err)
	}
	src, err := os.ReadFile(paths[1])
	if err != nil {
		t.Fatalf("read test: %v", err)
	}
	for _, want := range []string{
		"from capybara.replay import Session",
		"abcdef12_fixture.json",
		"def test_tool_contracts",
		"serve_tool",
	} {
		if !strings.Contains(string(src), want) {
			t.Errorf("generated test missing %q", want)
		}
	}
}

func TestWriteGoldenNamesByRun(t *testing.T) {
	fx := Fixture{Run: "abcdef1234", Source: "otlp"}
	dir := t.TempDir()
	path, err := WriteGolden(dir, fx)
	if err != nil {
		t.Fatalf("WriteGolden: %v", err)
	}
	if filepath.Base(path) != "golden_abcdef12.json" {
		t.Errorf("golden path = %s", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("golden not written: %v", err)
	}
}
