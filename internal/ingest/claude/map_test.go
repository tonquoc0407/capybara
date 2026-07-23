package claude

import (
	"strings"
	"testing"

	"github.com/tonquoc0407/capybara/internal/store"
)

// Lines mirror the on-disk Claude Code session format; the fixture is the
// contract with the real, unversioned log.
const session = `{"type":"queue-operation","operation":"enqueue","timestamp":"2026-07-22T10:00:00Z","content":"fix the bug"}
{"type":"user","uuid":"u1","parentUuid":null,"isSidechain":false,"timestamp":"2026-07-22T10:00:01Z","cwd":"/home/x/proj","gitBranch":"main","version":"2.1.0","message":{"role":"user","content":"fix the bug"}}
{"type":"assistant","uuid":"a1","parentUuid":"u1","isSidechain":false,"timestamp":"2026-07-22T10:00:05Z","requestId":"req_1","message":{"id":"msg_1","model":"claude-fable-5","role":"assistant","content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"On it."},{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"go test"}}],"usage":{"input_tokens":10,"cache_creation_input_tokens":100,"cache_read_input_tokens":1000,"output_tokens":50}}}
{"type":"user","uuid":"u2","parentUuid":"a1","isSidechain":false,"timestamp":"2026-07-22T10:00:09Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok\ttests pass"}]}}
{"type":"assistant","uuid":"a2","parentUuid":"u2","isSidechain":false,"timestamp":"2026-07-22T10:00:12Z","message":{"id":"msg_2","model":"claude-fable-5","role":"assistant","content":[{"type":"tool_use","id":"toolu_2","name":"Task","input":{"prompt":"explore the repo"}}],"usage":{"input_tokens":5,"output_tokens":9}}}
{"type":"user","uuid":"s1","parentUuid":null,"isSidechain":true,"timestamp":"2026-07-22T10:00:13Z","message":{"role":"user","content":"explore the repo"}}
{"type":"assistant","uuid":"s2","parentUuid":"s1","isSidechain":true,"timestamp":"2026-07-22T10:00:15Z","message":{"id":"msg_s1","model":"claude-haiku-4-5","role":"assistant","content":[{"type":"text","text":"found it"}],"usage":{"input_tokens":3,"output_tokens":4}}}
{"type":"user","uuid":"u3","parentUuid":"a2","isSidechain":false,"timestamp":"2026-07-22T10:00:20Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_2","content":[{"type":"text","text":"subagent report"}],"is_error":true}]}}
{"type":"summary","summary":"fix the login bug","leafUuid":"a2"}
`

func mapSession(t *testing.T, text string) (store.Batch, string) {
	t.Helper()
	m := newMapper("sess1", true)
	total := store.Batch{Source: "claude"}
	label := ""
	for _, ln := range strings.Split(strings.TrimSpace(text), "\n") {
		delta, l := m.process([]byte(ln))
		mergeBatch(&total, delta)
		if l != "" {
			label = l
		}
	}
	return total, label
}

func spanByID(b store.Batch, id string) *store.Span {
	for i := range b.Spans {
		if b.Spans[i].ID == id {
			return &b.Spans[i]
		}
	}
	return nil
}

func contentsOf(b store.Batch, spanID string) []store.Content {
	var out []store.Content
	for _, c := range b.Contents {
		if c.SpanID == spanID {
			out = append(out, c)
		}
	}
	return out
}

func TestMapsSessionTree(t *testing.T) {
	b, label := mapSession(t, session)
	if label != "fix the login bug" {
		t.Errorf("label = %q", label)
	}
	root := spanByID(b, "sess1:root")
	if root == nil || root.Kind != store.KindAgent || root.Name != "proj" {
		t.Fatalf("root = %+v", root)
	}
	llm := spanByID(b, "msg_1")
	if llm == nil || llm.Kind != store.KindLLM || llm.ParentID != "sess1:root" {
		t.Fatalf("llm = %+v", llm)
	}
	// Cache reads are context, not new tokens: 10 input + 100 cache_creation.
	if llm.TokensIn != 110 || llm.TokensOut != 50 {
		t.Errorf("llm tokens = %d/%d, want 110/50", llm.TokensIn, llm.TokensOut)
	}
	if u, ok := llm.Attrs.Raw["usage"].(usage); !ok || u.CacheReadTokens != 1000 {
		t.Errorf("raw usage lost the cache read: %+v", llm.Attrs.Raw["usage"])
	}
	if llm.Attrs.Model != "claude-fable-5" || llm.StartedAt.Equal(llm.EndedAt) {
		t.Errorf("llm attrs/times = %+v", llm)
	}
	tool := spanByID(b, "toolu_1")
	if tool == nil || tool.Kind != store.KindTool || tool.ParentID != "msg_1" ||
		tool.Attrs.ToolName != "Bash" || tool.Status != "ok" {
		t.Fatalf("tool = %+v", tool)
	}
	if tool.EndedAt.Sub(tool.StartedAt).Seconds() != 4 {
		t.Errorf("tool duration = %v", tool.EndedAt.Sub(tool.StartedAt))
	}
}

func TestMapsSubagentNesting(t *testing.T) {
	b, _ := mapSession(t, session)
	task := spanByID(b, "toolu_2")
	if task == nil || task.Attrs.ToolName != "Task" || task.Status != "error" {
		t.Fatalf("task = %+v", task)
	}
	side := spanByID(b, "msg_s1")
	if side == nil || side.ParentID != "toolu_2" {
		t.Fatalf("sidechain llm = %+v, want parent toolu_2", side)
	}
	sideContents := contentsOf(b, "msg_s1")
	if len(sideContents) != 2 || sideContents[0].Role != "user" ||
		sideContents[0].Body != "explore the repo" {
		t.Errorf("sidechain contents = %+v", sideContents)
	}
}

func TestMapsContents(t *testing.T) {
	b, _ := mapSession(t, session)
	llm := contentsOf(b, "msg_1")
	if len(llm) != 3 {
		t.Fatalf("llm contents = %+v", llm)
	}
	if llm[0].Role != "user" || llm[0].Body != "fix the bug" ||
		llm[1].Role != "thinking" || llm[2].Role != "assistant" {
		t.Errorf("llm contents = %+v", llm)
	}
	tool := contentsOf(b, "toolu_1")
	if len(tool) != 2 || tool[0].Role != "input" || tool[1].Role != "output" ||
		tool[1].Body != "ok\ttests pass" {
		t.Errorf("tool contents = %+v", tool)
	}
}

func TestParseErrorFindingKeepsGoing(t *testing.T) {
	broken := `not json at all
{"type":"user","uuid":"u1","timestamp":"2026-07-22T10:00:01Z","cwd":"/home/x/proj","message":{"role":"user","content":"hello"}}
{"type":"assistant","uuid":"a1","parentUuid":"u1","timestamp":"2026-07-22T10:00:03Z","message":{"id":"msg_1","model":"m","role":"assistant","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":1,"output_tokens":1}}}
`
	b, _ := mapSession(t, broken)
	if len(b.Findings) != 1 {
		t.Fatalf("findings = %+v", b.Findings)
	}
	f := b.Findings[0]
	if f.Type != "parse_error" || f.RunID != "sess1" || f.SpanID != "" ||
		!strings.Contains(f.Detail, `"line":1`) {
		t.Errorf("finding = %+v", f)
	}
	if spanByID(b, "msg_1") == nil {
		t.Error("mapping stopped after bad line")
	}
}

func TestSameMessageLinesDoNotDoubleCount(t *testing.T) {
	twoLines := `{"type":"assistant","uuid":"a1","timestamp":"2026-07-22T10:00:03Z","cwd":"/home/x/proj","message":{"id":"msg_1","model":"m","role":"assistant","content":[{"type":"text","text":"part one"}],"usage":{"input_tokens":7,"output_tokens":3}}}
{"type":"assistant","uuid":"a2","timestamp":"2026-07-22T10:00:04Z","message":{"id":"msg_1","model":"m","role":"assistant","content":[{"type":"tool_use","id":"toolu_9","name":"Read","input":{"file":"x"}}],"usage":{"input_tokens":7,"output_tokens":3}}}
`
	b, _ := mapSession(t, twoLines)
	llmCount := 0
	for _, sp := range b.Spans {
		if sp.Kind == store.KindLLM {
			llmCount++
		}
	}
	if llmCount != 1 {
		t.Fatalf("llm spans = %d, want 1", llmCount)
	}
	llm := spanByID(b, "msg_1")
	if llm.TokensIn != 7 || llm.TokensOut != 3 {
		t.Errorf("tokens = %d/%d, want 7/3", llm.TokensIn, llm.TokensOut)
	}
	if tool := spanByID(b, "toolu_9"); tool == nil || tool.ParentID != "msg_1" {
		t.Errorf("tool from second line = %+v", tool)
	}
}

func TestNoContentCapture(t *testing.T) {
	m := newMapper("sess1", false)
	total := store.Batch{}
	for _, ln := range strings.Split(strings.TrimSpace(session), "\n") {
		delta, _ := m.process([]byte(ln))
		mergeBatch(&total, delta)
	}
	if len(total.Contents) != 0 {
		t.Errorf("contents with capture off = %+v", total.Contents)
	}
	if spanByID(total, "msg_1") == nil {
		t.Error("spans missing with capture off")
	}
}

func TestMCPToolFlag(t *testing.T) {
	lines := `{"type":"assistant","uuid":"a1","timestamp":"2026-07-22T10:00:03Z","cwd":"/p","message":{"id":"msg_1","model":"m","role":"assistant","content":[{"type":"tool_use","id":"toolu_m","name":"mcp__github__get_pr","input":{}}]}}
`
	b, _ := mapSession(t, lines)
	tool := spanByID(b, "toolu_m")
	if tool == nil || !tool.Attrs.MCP || tool.Kind != store.KindTool {
		t.Errorf("mcp tool = %+v", tool)
	}
}
