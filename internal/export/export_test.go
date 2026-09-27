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

func TestWriteTypeScriptEmitsRunnableModule(t *testing.T) {
	fx := Fixture{Run: "abcdef1234", Source: "otlp", Tools: []ToolFixture{{
		Hash: "h1", SpanID: "t1", Tool: "fetch", Input: `{"sku":"A"}`,
		Output: `{"price":42}`, Schema: json.RawMessage(`{"type":["object"]}`),
	}}}
	dir := t.TempDir()
	paths, err := WriteTypeScript(dir, fx)
	if err != nil {
		t.Fatalf("WriteTypeScript: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("paths = %v, want fixture + test", paths)
	}
	if filepath.Base(paths[0]) != "abcdef12_fixture.json" {
		t.Errorf("fixture path = %s", paths[0])
	}
	if filepath.Base(paths[1]) != "abcdef12.test.ts" {
		t.Errorf("test path = %s", paths[1])
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
		"import { Session } from 'capybara-sdk';",
		"abcdef12_fixture.json",
		"node:test",
		"session.serveTool",
		"checkSchema",
	} {
		if !strings.Contains(string(src), want) {
			t.Errorf("generated test missing %q", want)
		}
	}
}

func TestCurlGeneratesCorrectCommands(t *testing.T) {
	st := openTemp(t)
	at := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)

	// Test 1: OpenAI
	bOpenAI := store.Batch{
		Source: "test",
		Spans: []store.Span{{
			ID: "llm-openai", RunID: "r-openai", Kind: store.KindLLM, Name: "chat",
			StartedAt: at, EndedAt: at.Add(time.Second), Status: "ok",
			Attrs: store.Attrs{Model: "gpt-4o", Provider: "openai"},
		}},
		Contents: []store.Content{
			{SpanID: "llm-openai", Role: "system", Seq: 0, Body: "You are a helper."},
			{SpanID: "llm-openai", Role: "user", Seq: 1, Body: "Hello world!"},
		},
	}
	if err := st.WriteBatch(context.Background(), bOpenAI); err != nil {
		t.Fatalf("WriteBatch openai: %v", err)
	}
	cmd, err := Curl(context.Background(), st, "r-openai")
	if err != nil {
		t.Fatalf("Curl openai: %v", err)
	}
	if !strings.Contains(cmd, "https://api.openai.com/v1/chat/completions") ||
		!strings.Contains(cmd, "$OPENAI_API_KEY") ||
		!strings.Contains(cmd, "Hello world!") ||
		!strings.Contains(cmd, "gpt-4o") {
		t.Errorf("OpenAI curl incorrect:\n%s", cmd)
	}

	// Test 2: Anthropic
	bAnthropic := store.Batch{
		Source: "test",
		Spans: []store.Span{{
			ID: "llm-anthropic", RunID: "r-anthropic", Kind: store.KindLLM, Name: "chat",
			StartedAt: at, EndedAt: at.Add(time.Second), Status: "ok",
			Attrs: store.Attrs{Model: "claude-3-5-sonnet", Provider: "anthropic"},
		}},
		Contents: []store.Content{
			{SpanID: "llm-anthropic", Role: "system", Seq: 0, Body: "You are Claude."},
			{SpanID: "llm-anthropic", Role: "user", Seq: 1, Body: "Hi Claude!"},
		},
	}
	if err := st.WriteBatch(context.Background(), bAnthropic); err != nil {
		t.Fatalf("WriteBatch anthropic: %v", err)
	}
	cmd, err = Curl(context.Background(), st, "r-anthropic")
	if err != nil {
		t.Fatalf("Curl anthropic: %v", err)
	}
	if !strings.Contains(cmd, "https://api.anthropic.com/v1/messages") ||
		!strings.Contains(cmd, "$ANTHROPIC_API_KEY") ||
		!strings.Contains(cmd, "Hi Claude!") ||
		!strings.Contains(cmd, "claude-3-5-sonnet") {
		t.Errorf("Anthropic curl incorrect:\n%s", cmd)
	}

	// Test 3: Google
	bGoogle := store.Batch{
		Source: "test",
		Spans: []store.Span{{
			ID: "llm-google", RunID: "r-google", Kind: store.KindLLM, Name: "chat",
			StartedAt: at, EndedAt: at.Add(time.Second), Status: "ok",
			Attrs: store.Attrs{Model: "gemini-2.5-flash", Provider: "google"},
		}},
		Contents: []store.Content{
			{SpanID: "llm-google", Role: "user", Seq: 0, Body: "Hi Gemini!"},
		},
	}
	if err := st.WriteBatch(context.Background(), bGoogle); err != nil {
		t.Fatalf("WriteBatch google: %v", err)
	}
	cmd, err = Curl(context.Background(), st, "r-google")
	if err != nil {
		t.Fatalf("Curl google: %v", err)
	}
	if !strings.Contains(cmd, "generativelanguage.googleapis.com") ||
		!strings.Contains(cmd, "$GEMINI_API_KEY") ||
		!strings.Contains(cmd, "Hi Gemini!") ||
		!strings.Contains(cmd, "gemini-2.5-flash") {
		t.Errorf("Google curl incorrect:\n%s", cmd)
	}

	// Test 4: Run with no LLM spans
	bNoLLM := store.Batch{
		Source: "test",
		Spans: []store.Span{{
			ID: "tool-only", RunID: "r-nollm", Kind: store.KindTool, Name: "tool",
			StartedAt: at, EndedAt: at.Add(time.Second), Status: "ok",
		}},
	}
	if err := st.WriteBatch(context.Background(), bNoLLM); err != nil {
		t.Fatalf("WriteBatch nollm: %v", err)
	}
	if _, err := Curl(context.Background(), st, "r-nollm"); err == nil {
		t.Error("Curl expected error for run without LLM spans")
	}
}
