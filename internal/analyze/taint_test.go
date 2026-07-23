package analyze

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

func improviseFinding() store.Finding {
	return store.Finding{
		RunID: "r1", SpanID: "r1-llm2", Type: "improvised", Severity: "error",
		Detail: `{"tool":"search_db","tool_span":"r1-tool"}`,
	}
}

func TestTaintRunLinksFailedToolToOutput(t *testing.T) {
	spans := improviseRunBatch("r1", "boom", "answer", "error").Spans
	edges := taintRun(spans, []store.Finding{improviseFinding()})
	got := make(map[[2]string]bool)
	for _, e := range edges {
		got[[2]string{e.SpanID, e.SourceSpanID}] = true
	}
	for _, want := range [][2]string{{"r1-llm2", "r1-tool"}, {"r1-root", "r1-llm2"}} {
		if !got[want] {
			t.Errorf("taint missing edge %s<-%s; got %+v", want[0], want[1], edges)
		}
	}
	// The llm turn that ran before the failure never consumed it.
	if got[[2]string{"r1-llm1", "r1-tool"}] {
		t.Errorf("tainted a turn that preceded the failure: %+v", edges)
	}
}

func TestBlameWalksOutputToRoot(t *testing.T) {
	spans := improviseRunBatch("r1", "boom", "answer", "error").Spans
	findings := []store.Finding{improviseFinding()}
	chain := blame("r1", spans, findings, taintRun(spans, findings))
	if len(chain.Hops) != 3 {
		t.Fatalf("hops = %d, want 3: %+v", len(chain.Hops), chain.Hops)
	}
	if chain.Hops[0].Span.ID != "r1-root" {
		t.Errorf("first hop = %s, want r1-root", chain.Hops[0].Span.ID)
	}
	root := chain.Hops[len(chain.Hops)-1]
	if root.Span.ID != "r1-tool" || !root.Root {
		t.Errorf("root hop = %+v, want r1-tool marked root", root)
	}
}

func TestBlameBranchesOnMultipleSources(t *testing.T) {
	spans := []store.Span{
		{
			ID: "root", RunID: "r1", Kind: store.KindAgent, Name: "agent",
			StartedAt: t0, EndedAt: t0.Add(10 * time.Second),
		},
		{
			ID: "llm1", RunID: "r1", ParentID: "root", Kind: store.KindLLM, Name: "chat",
			StartedAt: t0, EndedAt: t0.Add(time.Second),
		},
		{
			ID: "toolA", RunID: "r1", ParentID: "llm1", Kind: store.KindTool, Name: "a",
			StartedAt: t0.Add(2 * time.Second), EndedAt: t0.Add(3 * time.Second), Status: "error",
		},
		{
			ID: "toolB", RunID: "r1", ParentID: "llm1", Kind: store.KindTool, Name: "b",
			StartedAt: t0.Add(3 * time.Second), EndedAt: t0.Add(4 * time.Second), Status: "error",
		},
		{
			ID: "llm2", RunID: "r1", ParentID: "root", Kind: store.KindLLM, Name: "chat",
			StartedAt: t0.Add(5 * time.Second), EndedAt: t0.Add(6 * time.Second),
		},
	}
	findings := []store.Finding{
		{RunID: "r1", SpanID: "toolA", Type: "empty_payload", Severity: "warn"},
		{RunID: "r1", SpanID: "toolB", Type: "malformed", Severity: "warn"},
	}
	chain := blame("r1", spans, findings, taintRun(spans, findings))
	var got []string
	for _, hop := range chain.Hops {
		got = append(got, fmt.Sprintf("%s@%d", hop.Span.ID, hop.Depth))
	}
	want := []string{"root@0", "llm2@1", "toolA@2", "toolB@2"}
	if !slices.Equal(got, want) {
		t.Fatalf("hops = %v, want %v", got, want)
	}
	for _, hop := range chain.Hops[2:] {
		if !hop.Root {
			t.Errorf("%s is a source but not marked root", hop.Span.ID)
		}
	}
}

func TestBlameUntaintedOutputIsEmpty(t *testing.T) {
	spans := improviseRunBatch("r1", "boom", "answer", "ok").Spans
	if chain := blame("r1", spans, nil, nil); len(chain.Hops) != 0 {
		t.Errorf("untainted output produced hops: %+v", chain.Hops)
	}
}

func TestSweepPersistsTaintsIdempotently(t *testing.T) {
	st := openTemp(t)
	b := improviseRunBatch("r1", "connection refused",
		"The search_db results show the price is 42.", "error")
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	sweep(t, st)
	first, err := st.Taints(context.Background(), "r1")
	if err != nil {
		t.Fatalf("Taints: %v", err)
	}
	if len(first) == 0 {
		t.Fatal("sweep recorded no taints")
	}
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	sweep(t, st)
	second, err := st.Taints(context.Background(), "r1")
	if err != nil {
		t.Fatalf("Taints: %v", err)
	}
	if len(second) != len(first) {
		t.Fatalf("taints after re-sweep = %d, want %d", len(second), len(first))
	}
}

func TestBlameAfterSweepReachesTool(t *testing.T) {
	st := openTemp(t)
	b := improviseRunBatch("r1", "connection refused",
		"The search_db results show the price is 42.", "error")
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	sweep(t, st)
	chain, err := Blame(context.Background(), st, "r1")
	if err != nil {
		t.Fatalf("Blame: %v", err)
	}
	last := chain.Hops[len(chain.Hops)-1]
	if last.Span.ID != "r1-tool" || !last.Root {
		t.Fatalf("blame root = %+v, want r1-tool", last)
	}
}
