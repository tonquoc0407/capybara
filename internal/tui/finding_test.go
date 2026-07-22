package tui

import (
	"strings"
	"testing"

	"github.com/tonquoc0407/capybara/internal/store"
	"github.com/tonquoc0407/capybara/internal/theme"
)

func driftFinding(spanID string) store.Finding {
	return store.Finding{
		RunID: "r1", SpanID: spanID, Type: "drift", Severity: "warning",
		Detail: `{"tool":"fetch_api","missing":["price"],"retyped":[{"field":"qty","want":"number","got":"string"}]}`,
	}
}

func TestFindingSummaries(t *testing.T) {
	cases := map[string]store.Finding{
		"missing field: price (+1)": driftFinding("t1"),
		"malformed output, want object": {
			Type: "malformed", Detail: `{"tool":"fetch_api","want":"object"}`,
		},
		"empty payload": {Type: "empty_payload", Detail: `{"tool":"x"}`},
		"improvised after fetch_api failure": {
			Type: "improvised", Detail: `{"tool":"fetch_api","cause":"error","evidence":"output mentions fetch_api"}`,
		},
		"parse error at line 7": {Type: "parse_error", Detail: `{"line":7,"error":"bad json"}`},
	}
	for want, f := range cases {
		if got := findingSummary(f); got != want {
			t.Errorf("summary(%s) = %q, want %q", f.Type, got, want)
		}
	}
}

func TestTreeShowsFindingNotes(t *testing.T) {
	m := newTree(theme.Bara())
	m.setSize(60, 20)
	spans := []store.Span{
		span("root", "", store.KindAgent, "agent_loop", 0, 10),
		span("tool1", "root", store.KindTool, "fetch_api", 1, 1),
		span("tool2", "root", store.KindTool, "clean", 3, 1),
	}
	m.setSpans(spans, map[string][]store.Finding{"tool1": {driftFinding("tool1")}})
	if len(m.rows) != 4 {
		t.Fatalf("rows = %d, want 4 (3 spans + 1 note)", len(m.rows))
	}
	if m.rows[2].note == "" || !strings.Contains(m.rows[2].note, "missing field: price") {
		t.Errorf("note row = %+v", m.rows[2])
	}
	if !strings.Contains(m.renderRow(1), "!") {
		t.Errorf("span with finding lacks ! mark: %q", m.renderRow(1))
	}
	m.sel = 1
	m.move(1) // must skip the note row
	if got := m.selectedID(); got != "tool2" {
		t.Errorf("selection after move = %q, want tool2", got)
	}
	m.move(-1)
	if got := m.selectedID(); got != "tool1" {
		t.Errorf("selection after move back = %q, want tool1", got)
	}
}

func TestDetailShowsFindingDiff(t *testing.T) {
	m := newDetail(theme.Bara())
	m.setSize(60, 24)
	sp := span("tool1", "root", store.KindTool, "fetch_api", 1, 1)
	m.setSpan(sp, []store.Content{
		{SpanID: "tool1", Role: "output", Seq: 0, Body: `{"qty":"3"}`, MediaType: "application/json"},
	}, []store.Finding{driftFinding("tool1")})
	out := plainView(m)
	for _, want := range []string{"drift: missing field: price", "missing: price", "qty: want number, got string"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail missing %q:\n%s", want, out)
		}
	}
}

func TestDetailNoticesSkippedAnalysis(t *testing.T) {
	m := newDetail(theme.Bara())
	m.setSize(60, 24)
	m.setSpan(span("tool1", "root", store.KindTool, "fetch_api", 1, 1), nil, nil)
	if !strings.Contains(plainView(m), "contract analysis skipped") {
		t.Error("missing analysis-skipped notice")
	}
	// An llm span without content gets no such notice.
	m.setSpan(span("llm1", "root", store.KindLLM, "chat", 1, 1), nil, nil)
	if strings.Contains(plainView(m), "contract analysis skipped") {
		t.Error("notice shown for non-tool span")
	}
}
