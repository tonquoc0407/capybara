package replay

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
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

func TestRunExecutesTheRecordedRuntimeAndLinksItsParent(t *testing.T) {
	st := openTemp(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	recorded(t, st)
	m, err := Build(ctx, st, "r1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	python, cwd, marker := fakePythonReplay(t)
	m.Entrypoint = []string{python, "agent.py"}
	m.Cwd = cwd
	err = Run(ctx, st, m, true)
	if err == nil || !strings.Contains(err.Error(), "fake replay ran") {
		t.Fatalf("Run = %v, want the fake runner's failure", err)
	}
	raw, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("runner marker: %v", err)
	}
	var handedOff Manifest
	if err := json.Unmarshal(raw, &handedOff); err != nil {
		t.Fatalf("runner received invalid manifest: %v", err)
	}
	if handedOff.RunID != m.RunID || handedOff.ParentRunID != "r1" || handedOff.Endpoint == "" {
		t.Fatalf("runner manifest = %+v", handedOff)
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

// fakePythonReplay provides the module Run invokes, using the platform's own
// Python rather than a shell script. It records the handed-off manifest, then
// fails deliberately so the test does not wait for spans that it never emits.
func fakePythonReplay(t *testing.T) (python, cwd, marker string) {
	t.Helper()
	for _, name := range []string{"python3", "python"} {
		path, err := exec.LookPath(name)
		if err == nil {
			python = path
			break
		}
	}
	if python == "" {
		t.Skip("Python is required to exercise the Python replay runner")
	}
	cwd = t.TempDir()
	pkg := filepath.Join(cwd, "capybara")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "__init__.py"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	marker = filepath.Join(cwd, "runner marker.json")
	source := "" +
		"import pathlib, sys\n" +
		"pathlib.Path('runner marker.json').write_bytes(pathlib.Path(sys.argv[1]).read_bytes())\n" +
		"print('fake replay ran', file=sys.stderr)\n" +
		"raise SystemExit(23)\n"
	if err := os.WriteFile(filepath.Join(pkg, "replay.py"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	return python, cwd, marker
}
