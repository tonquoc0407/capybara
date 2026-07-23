package replay

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

// A capybara already holding the default port must not receive the replay's
// spans into its own database.
func TestServeTakesAPortOfItsOwn(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:4318")
	if err != nil {
		t.Skip("default otlp port already in use")
	}
	defer busy.Close()
	stop, endpoint, err := serve(context.Background(), openTemp(t), true)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	defer stop()
	if endpoint == "" || strings.Contains(endpoint, ":4318") {
		t.Errorf("endpoint = %q, want a port of its own", endpoint)
	}
}

var t0 = time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)

func openTemp(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// recorded writes a run shaped like one the SDK produces: an agent root that
// says how it was launched, one model call and one tool call.
func recorded(t *testing.T, st *store.Store) {
	t.Helper()
	entrypoint, err := json.Marshal([]string{"/usr/bin/python3", "agent.py"})
	if err != nil {
		t.Fatal(err)
	}
	batch := store.Batch{
		Source: "otlp",
		Spans: []store.Span{
			{
				ID: "root", RunID: "r1", Kind: store.KindAgent, Name: "agent",
				StartedAt: t0, EndedAt: t0.Add(3 * time.Second), Status: "ok",
				Attrs: store.Attrs{Raw: map[string]any{
					entrypointAttr: string(entrypoint),
					cwdAttr:        "/tmp/agent",
				}},
			},
			{
				ID: "llm1", RunID: "r1", ParentID: "root", Kind: store.KindLLM,
				Name: "chat", StartedAt: t0.Add(time.Second), EndedAt: t0.Add(2 * time.Second),
				Status: "ok", Attrs: store.Attrs{Model: "claude-sonnet-5"},
			},
			{
				ID: "tool1", RunID: "r1", ParentID: "root", Kind: store.KindTool,
				Name: "execute_tool lookup", StartedAt: t0.Add(2 * time.Second),
				EndedAt: t0.Add(3 * time.Second), Status: "ok",
				Attrs: store.Attrs{ToolName: "lookup"},
			},
		},
		Contents: []store.Content{
			{SpanID: "llm1", Role: "user", Seq: 0, Body: "what does it cost?", MediaType: "text/plain"},
			{SpanID: "llm1", Role: "assistant", Seq: 1, Body: `[{"type":"text","content":"42"}]`, MediaType: "application/json"},
			{SpanID: "tool1", Role: "input", Seq: 0, Body: `{"sku":"A"}`, MediaType: "application/json"},
			{SpanID: "tool1", Role: "output", Seq: 1, Body: `{"price":42}`, MediaType: "application/json"},
		},
	}
	if err := st.WriteBatch(context.Background(), batch); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestBuildCacheStoresRecordedReplies(t *testing.T) {
	st := openTemp(t)
	recorded(t, st)
	n, err := BuildCache(context.Background(), st, "r1")
	if err != nil {
		t.Fatalf("BuildCache: %v", err)
	}
	if n != 1 {
		t.Fatalf("cached %d entries, want 1", n)
	}
	cached, err := st.LLMCache(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	if cached[0].Response != `[{"type":"text","content":"42"}]` {
		t.Errorf("response = %q", cached[0].Response)
	}
	want := HashLLMRequest("claude-sonnet-5", []Message{{Role: "user", Body: "what does it cost?"}})
	if cached[0].RequestHash != want {
		t.Errorf("hash = %s, want %s", cached[0].RequestHash, want)
	}
}

// A model call whose reply was never recorded cannot be served, so caching it
// would make replay invent one.
func TestBuildCacheSkipsSpansWithoutAReply(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	batch := store.Batch{
		Source: "otlp",
		Spans: []store.Span{{
			ID: "llm1", RunID: "r2", Kind: store.KindLLM, Name: "chat",
			StartedAt: t0, EndedAt: t0.Add(time.Second), Status: "ok",
		}},
		Contents: []store.Content{
			{SpanID: "llm1", Role: "user", Seq: 0, Body: "hi", MediaType: "text/plain"},
		},
	}
	if err := st.WriteBatch(ctx, batch); err != nil {
		t.Fatal(err)
	}
	n, err := BuildCache(ctx, st, "r2")
	if err != nil {
		t.Fatalf("BuildCache: %v", err)
	}
	if n != 0 {
		t.Fatalf("cached %d entries, want 0", n)
	}
}

func TestBuildManifest(t *testing.T) {
	st := openTemp(t)
	recorded(t, st)
	m, err := Build(context.Background(), st, "r1", "tool1", `{"price":99}`)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if m.ParentRunID != "r1" || len(m.RunID) != 32 {
		t.Errorf("run ids: parent %q, new %q", m.ParentRunID, m.RunID)
	}
	if got := m.Entrypoint; len(got) != 2 || got[1] != "agent.py" {
		t.Errorf("entrypoint = %v", got)
	}
	if m.Cwd != "/tmp/agent" {
		t.Errorf("cwd = %q", m.Cwd)
	}
	if len(m.LLM) != 1 || len(m.Tools) != 1 {
		t.Fatalf("manifest has %d llm and %d tool entries", len(m.LLM), len(m.Tools))
	}
	tool := m.Tools[0]
	if !tool.Edited || tool.Output != `{"price":99}` {
		t.Errorf("edit not applied: %+v", tool)
	}
	if tool.Hash != HashToolCall("lookup", `{"sku":"A"}`) {
		t.Error("tool hash does not cover the recorded arguments")
	}
}

func TestBuildRejectsAnEditWithNoRecordedOutput(t *testing.T) {
	st := openTemp(t)
	recorded(t, st)
	_, err := Build(context.Background(), st, "r1", "llm1", `{"price":99}`)
	if err == nil {
		t.Fatal("editing a span with no tool output should fail")
	}
}

// Replay re-executes a process, so a run that never recorded one cannot be
// replayed however complete its spans are.
func TestBuildRequiresARecordedEntrypoint(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	batch := store.Batch{
		Source: "otlp",
		Spans: []store.Span{{
			ID: "root", RunID: "r3", Kind: store.KindAgent, Name: "agent",
			StartedAt: t0, EndedAt: t0.Add(time.Second), Status: "ok",
		}},
	}
	if err := st.WriteBatch(ctx, batch); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(ctx, st, "r3", "", ""); err == nil {
		t.Fatal("a run without an entrypoint should not be replayable")
	}
}

func TestRunLinksTheReplayToItsParent(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	recorded(t, st)
	m, err := Build(ctx, st, "r1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	m.Entrypoint = []string{"/bin/sh", "-c", "exit 0"}
	if err := Run(ctx, st, m, true); err == nil {
		t.Log("runner exited cleanly")
	}
	runs, err := st.ListRuns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range runs {
		if r.ID == m.RunID {
			if r.ParentRunID != "r1" {
				t.Errorf("parent = %q, want r1", r.ParentRunID)
			}
			return
		}
	}
	t.Fatal("replay run was not created")
}
