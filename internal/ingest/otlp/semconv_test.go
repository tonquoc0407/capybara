package otlp

import (
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/tonquoc0407/capybara/internal/store"
)

// Tests speak the wire conventions on purpose: they pin what external
// emitters send, independent of the constants in semconv.go.

func singleSpan() (ptrace.Traces, ptrace.Span) {
	td := ptrace.NewTraces()
	span := td.ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetTraceID(pcommon.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	span.SetSpanID(pcommon.SpanID{1, 2, 3, 4, 5, 6, 7, 8})
	t0 := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(t0))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(t0.Add(time.Second)))
	return td, span
}

func TestMapsChatSpan(t *testing.T) {
	td, span := singleSpan()
	span.SetName("chat fake-gpt")
	span.Attributes().PutStr("gen_ai.operation.name", "chat")
	span.Attributes().PutStr("gen_ai.request.model", "fake-gpt")
	span.Attributes().PutStr("gen_ai.provider.name", "fake")
	span.Attributes().PutInt("gen_ai.usage.input_tokens", 100)
	span.Attributes().PutInt("gen_ai.usage.output_tokens", 20)
	b := ToBatch(td, true)
	if len(b.Spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(b.Spans))
	}
	sp := b.Spans[0]
	if sp.Kind != store.KindLLM {
		t.Errorf("kind = %q, want llm", sp.Kind)
	}
	if sp.Attrs.Model != "fake-gpt" || sp.Attrs.Provider != "fake" {
		t.Errorf("attrs = %+v", sp.Attrs)
	}
	if sp.TokensIn != 100 || sp.TokensOut != 20 {
		t.Errorf("tokens = %d/%d, want 100/20", sp.TokensIn, sp.TokensOut)
	}
	if sp.RunID != "0102030405060708090a0b0c0d0e0f10" || sp.ID != "0102030405060708" {
		t.Errorf("ids = %q / %q", sp.RunID, sp.ID)
	}
	if sp.ParentID != "" {
		t.Errorf("parent = %q, want empty", sp.ParentID)
	}
	if sp.Attrs.Raw["gen_ai.operation.name"] != "chat" {
		t.Errorf("raw attrs not preserved: %+v", sp.Attrs.Raw)
	}
}

func TestMapsLegacyNames(t *testing.T) {
	td, span := singleSpan()
	span.Attributes().PutStr("gen_ai.operation.name", "chat")
	span.Attributes().PutStr("gen_ai.system", "openai")
	span.Attributes().PutInt("gen_ai.usage.prompt_tokens", 7)
	span.Attributes().PutInt("gen_ai.usage.completion_tokens", 3)
	sp := ToBatch(td, true).Spans[0]
	if sp.Attrs.Provider != "openai" {
		t.Errorf("provider = %q, want openai", sp.Attrs.Provider)
	}
	if sp.TokensIn != 7 || sp.TokensOut != 3 {
		t.Errorf("tokens = %d/%d, want 7/3", sp.TokensIn, sp.TokensOut)
	}
}

func TestMapsToolSpan(t *testing.T) {
	td, span := singleSpan()
	span.Attributes().PutStr("gen_ai.operation.name", "execute_tool")
	span.Attributes().PutStr("gen_ai.tool.name", "search_db")
	sp := ToBatch(td, true).Spans[0]
	if sp.Kind != store.KindTool || sp.Attrs.ToolName != "search_db" {
		t.Errorf("span = %+v", sp)
	}
}

func TestMapsAgentSpanAndErrorStatus(t *testing.T) {
	td, span := singleSpan()
	span.Attributes().PutStr("gen_ai.operation.name", "invoke_agent")
	span.Status().SetCode(ptrace.StatusCodeError)
	sp := ToBatch(td, true).Spans[0]
	if sp.Kind != store.KindAgent || sp.Status != "error" {
		t.Errorf("span = %+v", sp)
	}
}

func TestUnknownSpanPassesThroughAsOther(t *testing.T) {
	td, span := singleSpan()
	span.SetName("db.query")
	span.Attributes().PutStr("db.system", "postgresql")
	sp := ToBatch(td, true).Spans[0]
	if sp.Kind != store.KindOther || sp.Name != "db.query" {
		t.Errorf("span = %+v", sp)
	}
	if sp.Attrs.Raw["db.system"] != "postgresql" {
		t.Errorf("raw attrs not preserved: %+v", sp.Attrs.Raw)
	}
}

func TestMCPSpanIsToolWithFlag(t *testing.T) {
	td, span := singleSpan()
	span.Attributes().PutStr("mcp.method.name", "tools/call")
	span.Attributes().PutStr("mcp.tool.name", "fetch")
	sp := ToBatch(td, true).Spans[0]
	if sp.Kind != store.KindTool || !sp.Attrs.MCP || sp.Attrs.ToolName != "fetch" {
		t.Errorf("span = %+v", sp)
	}
}

func TestCapturesContentEvents(t *testing.T) {
	td, span := singleSpan()
	span.Attributes().PutStr("gen_ai.operation.name", "chat")
	legacy := span.Events().AppendEmpty()
	legacy.SetName("gen_ai.content.prompt")
	legacy.Attributes().PutStr("gen_ai.prompt", "what is a capybara")
	current := span.Events().AppendEmpty()
	current.SetName("gen_ai.choice")
	current.Attributes().PutStr("gen_ai.event.content", `{"content":"a large rodent"}`)
	b := ToBatch(td, true)
	if len(b.Contents) != 2 {
		t.Fatalf("got %d contents, want 2", len(b.Contents))
	}
	if b.Contents[0].Role != "user" || b.Contents[0].Body != "what is a capybara" ||
		b.Contents[0].MediaType != "text/plain" {
		t.Errorf("contents[0] = %+v", b.Contents[0])
	}
	if b.Contents[1].Role != "assistant" || b.Contents[1].MediaType != "application/json" {
		t.Errorf("contents[1] = %+v", b.Contents[1])
	}
	if b.Contents[0].SpanID != b.Spans[0].ID || b.Contents[1].Seq != 1 {
		t.Errorf("contents keys = %+v", b.Contents)
	}
}

func TestCapturesMessageAttributes(t *testing.T) {
	td, span := singleSpan()
	span.Attributes().PutStr("gen_ai.operation.name", "chat")
	span.Attributes().PutStr("gen_ai.input.messages",
		`[{"role":"user","parts":[{"type":"text","content":"what does it cost?"}]}]`)
	span.Attributes().PutStr("gen_ai.output.messages",
		`[{"role":"assistant","parts":[{"type":"text","content":"42"}],"finish_reason":""}]`)
	b := ToBatch(td, true)
	if len(b.Contents) != 2 {
		t.Fatalf("got %d contents, want 2: %+v", len(b.Contents), b.Contents)
	}
	if b.Contents[0].Role != "user" || !strings.Contains(b.Contents[0].Body, "what does it cost?") {
		t.Errorf("contents[0] = %+v", b.Contents[0])
	}
	if b.Contents[1].Role != "assistant" || b.Contents[1].MediaType != "application/json" {
		t.Errorf("contents[1] = %+v", b.Contents[1])
	}
}

func TestCapturesSystemInstructions(t *testing.T) {
	td, span := singleSpan()
	span.Attributes().PutStr("gen_ai.operation.name", "chat")
	span.Attributes().PutStr("gen_ai.system_instructions",
		`[{"type":"text","content":"You are terse."}]`)
	b := ToBatch(td, true)
	if len(b.Contents) != 1 {
		t.Fatalf("got %d contents, want 1: %+v", len(b.Contents), b.Contents)
	}
	if b.Contents[0].Role != "system" || !strings.Contains(b.Contents[0].Body, "You are terse.") {
		t.Errorf("contents[0] = %+v", b.Contents[0])
	}
}

func TestCapturesToolCallIO(t *testing.T) {
	td, span := singleSpan()
	span.Attributes().PutStr("gen_ai.operation.name", "execute_tool")
	span.Attributes().PutStr("gen_ai.tool.call.arguments", `{"query":"gadget"}`)
	span.Attributes().PutStr("gen_ai.tool.call.result", `{"price":42}`)
	b := ToBatch(td, true)
	if len(b.Contents) != 2 {
		t.Fatalf("got %d contents, want 2: %+v", len(b.Contents), b.Contents)
	}
	if b.Contents[0].Role != "input" || b.Contents[1].Role != "output" ||
		b.Contents[1].Body != `{"price":42}` {
		t.Errorf("contents = %+v", b.Contents)
	}
}

func TestOpenInferenceStructuredMessagesPreferClean(t *testing.T) {
	td, span := singleSpan()
	span.Attributes().PutStr("openinference.span.kind", "LLM")
	span.Attributes().PutStr("llm.input_messages.0.message.role", "system")
	span.Attributes().PutStr("llm.input_messages.0.message.content", "You are terse.")
	span.Attributes().PutStr("llm.input_messages.1.message.role", "user")
	span.Attributes().PutStr("llm.input_messages.1.message.content", "price of NVDA?")
	span.Attributes().PutStr("llm.output_messages.0.message.role", "assistant")
	span.Attributes().PutStr("llm.output_messages.0.message.content", "$875.00")
	// The raw blob carries the whole ChatCompletion JSON and must be ignored
	// once the structured turns are present.
	span.Attributes().PutStr("output.value", `{"choices":[{"message":{"content":"$875.00"}}]}`)
	span.Attributes().PutStr("input.value", `{"messages":[{"role":"user"}]}`)
	b := ToBatch(td, true)
	if len(b.Contents) != 3 {
		t.Fatalf("got %d contents, want 3: %+v", len(b.Contents), b.Contents)
	}
	want := []store.Content{
		{Role: "system", Body: "You are terse."},
		{Role: "user", Body: "price of NVDA?"},
		{Role: "assistant", Body: "$875.00"},
	}
	for i, w := range want {
		if b.Contents[i].Role != w.Role || b.Contents[i].Body != w.Body {
			t.Errorf("contents[%d] = %+v, want role=%q body=%q", i, b.Contents[i], w.Role, w.Body)
		}
	}
}

func TestOpenInferenceToolSpanKeepsRawBlob(t *testing.T) {
	td, span := singleSpan()
	span.Attributes().PutStr("openinference.span.kind", "TOOL")
	span.Attributes().PutStr("tool.name", "get_stock_price")
	span.Attributes().PutStr("input.value", `{"symbol":"NVDA"}`)
	span.Attributes().PutStr("output.value", `{"status":502,"error":"upstream quote feed unavailable"}`)
	b := ToBatch(td, true)
	if len(b.Contents) != 2 {
		t.Fatalf("got %d contents, want 2: %+v", len(b.Contents), b.Contents)
	}
	if b.Contents[0].Role != "input" || b.Contents[0].Body != `{"symbol":"NVDA"}` {
		t.Errorf("contents[0] = %+v", b.Contents[0])
	}
	if b.Contents[1].Role != "output" || !strings.Contains(b.Contents[1].Body, "upstream quote feed") {
		t.Errorf("contents[1] = %+v", b.Contents[1])
	}
}

func TestNoContentDropsEvents(t *testing.T) {
	td, span := singleSpan()
	ev := span.Events().AppendEmpty()
	ev.SetName("gen_ai.content.prompt")
	ev.Attributes().PutStr("gen_ai.prompt", "secret")
	if b := ToBatch(td, false); len(b.Contents) != 0 {
		t.Errorf("got %d contents with capture off, want 0", len(b.Contents))
	}
}
