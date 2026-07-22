package intake

import (
	"context"
	"strings"
	"testing"

	"github.com/tonquoc0407/capybara/internal/store"
)

const replayGolden = `{
  "agent_name": "support-bot",
  "agent_version": "1.2.0",
  "session_id": "sess-42",
  "trigger": "webhook",
  "status": "failed",
  "input": {"question": "why is my bill high"},
  "output": null,
  "started_at": "2026-07-22T10:00:00Z",
  "ended_at": "2026-07-22T10:00:30Z",
  "total_cost_usd": 0.042,
  "error": {"message": "tool timeout"},
  "steps": [
    {"step_number": 1, "step_type": "llm_call", "name": "plan", "input": {"prompt": "plan it"}, "output": {"text": "will search"}, "duration_ms": 900, "tokens_used": 350},
    {"step_number": 2, "step_type": "retrieval", "name": "billing_docs", "input": {"query": "high bill"}, "output": {"hits": 3}, "duration_ms": 120},
    {"step_number": 3, "step_type": "tool_call", "name": "get_invoice", "parent_step": 1, "input": {"account": "a1"}, "output": {"amount": 900}, "duration_ms": 4000},
    {"step_number": 4, "step_type": "error", "name": "tool timeout", "caused_by_step": 3}
  ]
}`

func TestImportReplayTrace(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	if err := ImportReplay(ctx, st, strings.NewReader(replayGolden), true); err != nil {
		t.Fatalf("ImportReplay: %v", err)
	}
	runs, err := st.ListRuns(ctx)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != "sess-42" || runs[0].Source != "agent-replay" {
		t.Fatalf("runs = %+v", runs)
	}
	if runs[0].Label != "support-bot" || runs[0].Status != "error" {
		t.Errorf("run = %+v", runs[0])
	}
	spans, err := st.Spans(ctx, "sess-42")
	if err != nil {
		t.Fatalf("Spans: %v", err)
	}
	if len(spans) != 5 {
		t.Fatalf("got %d spans, want 5", len(spans))
	}
	byID := map[string]store.Span{}
	for _, sp := range spans {
		byID[sp.ID] = sp
	}
	root := byID["sess-42:root"]
	if root.Kind != store.KindAgent || root.Status != "error" ||
		root.CostUSD == nil || *root.CostUSD != 0.042 {
		t.Errorf("root = %+v", root)
	}
	if byID["sess-42:step:000001"].Kind != store.KindLLM {
		t.Errorf("step 1 = %+v", byID["sess-42:step:000001"])
	}
	if byID["sess-42:step:000002"].Kind != store.KindRetrieval {
		t.Errorf("step 2 = %+v", byID["sess-42:step:000002"])
	}
	tool := byID["sess-42:step:000003"]
	if tool.Kind != store.KindTool || tool.ParentID != "sess-42:step:000001" ||
		tool.Attrs.ToolName != "get_invoice" {
		t.Errorf("step 3 = %+v", tool)
	}
	if byID["sess-42:step:000004"].Status != "error" {
		t.Errorf("step 4 = %+v", byID["sess-42:step:000004"])
	}
	contents, err := st.Contents(ctx, "sess-42:step:000003")
	if err != nil {
		t.Fatalf("Contents: %v", err)
	}
	if len(contents) != 2 || contents[0].Role != "input" || contents[1].Role != "output" {
		t.Errorf("tool contents = %+v", contents)
	}
	rootContents, err := st.Contents(ctx, "sess-42:root")
	if err != nil {
		t.Fatalf("Contents: %v", err)
	}
	if len(rootContents) != 2 || rootContents[1].Role != "error" {
		t.Errorf("root contents = %+v", rootContents)
	}
}

func TestImportReplayArrayAndDerivedID(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	in := `[{"agent_name": "bot-a", "status": "completed", "started_at": "2026-07-22T10:00:00Z", "ended_at": "2026-07-22T10:00:05Z", "steps": []}]`
	if err := ImportReplay(ctx, st, strings.NewReader(in), true); err != nil {
		t.Fatalf("ImportReplay: %v", err)
	}
	runs, err := st.ListRuns(ctx)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 || !strings.HasPrefix(runs[0].ID, "replay-") || runs[0].Status != "ok" {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestImportReplayRejectsGarbage(t *testing.T) {
	st := openTemp(t)
	err := ImportReplay(context.Background(), st, strings.NewReader(`{"foo": 1}`), true)
	if err == nil || !strings.Contains(err.Error(), "agent_name") {
		t.Fatalf("err = %v, want missing agent_name", err)
	}
}
