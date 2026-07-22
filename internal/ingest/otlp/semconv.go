// Package otlp receives OTLP traces on localhost and maps gen_ai semantic
// conventions onto capybara's internal span model.
package otlp

import (
	"encoding/json"
	"strconv"
	"strings"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/tonquoc0407/capybara/internal/store"
)

// All gen_ai and mcp convention strings live in this file and nowhere else.
// Pinned at semconv v1.42 (post repo split), plus legacy dual-emission names.
const (
	attrOperationName    = "gen_ai.operation.name"
	attrRequestModel     = "gen_ai.request.model"
	attrResponseModel    = "gen_ai.response.model"
	attrInputTokens      = "gen_ai.usage.input_tokens"
	attrOutputTokens     = "gen_ai.usage.output_tokens"
	attrPromptTokens     = "gen_ai.usage.prompt_tokens"     // legacy
	attrCompletionTokens = "gen_ai.usage.completion_tokens" // legacy
	attrProviderName     = "gen_ai.provider.name"
	attrSystem           = "gen_ai.system" // legacy provider
	attrToolName         = "gen_ai.tool.name"
	attrMCPToolName      = "mcp.tool.name"
	mcpAttrPrefix        = "mcp."
	attrSystemInstr      = "gen_ai.system_instructions"
	attrInputMessages    = "gen_ai.input.messages"
	attrOutputMessages   = "gen_ai.output.messages"
	attrToolArguments    = "gen_ai.tool.call.arguments"
	attrToolResult       = "gen_ai.tool.call.result"
)

var kindByOperation = map[string]store.Kind{
	"invoke_agent":     store.KindAgent,
	"create_agent":     store.KindAgent,
	"chat":             store.KindLLM,
	"generate_content": store.KindLLM,
	"text_completion":  store.KindLLM,
	"embeddings":       store.KindLLM,
	"execute_tool":     store.KindTool,
}

var roleByEvent = map[string]string{
	"gen_ai.system.message":     "system",
	"gen_ai.user.message":       "user",
	"gen_ai.assistant.message":  "assistant",
	"gen_ai.tool.message":       "tool",
	"gen_ai.choice":             "assistant",
	"gen_ai.content.prompt":     "user",      // legacy
	"gen_ai.content.completion": "assistant", // legacy
}

// Event attributes that carry the message body, in preference order.
var bodyAttrs = []string{
	"gen_ai.event.content",
	"gen_ai.prompt",     // legacy
	"gen_ai.completion", // legacy
	"content",
	"body",
}

type mappedSpan struct {
	kind      store.Kind
	attrs     store.Attrs
	tokensIn  int64
	tokensOut int64
}

func mapSemconv(span ptrace.Span) mappedSpan {
	attrs := span.Attributes()
	m := mappedSpan{
		kind: store.KindOther,
		attrs: store.Attrs{
			Model:    strAttr(attrs, attrRequestModel, attrResponseModel),
			Provider: strAttr(attrs, attrProviderName, attrSystem),
			ToolName: strAttr(attrs, attrToolName, attrMCPToolName),
		},
		tokensIn:  intAttr(attrs, attrInputTokens, attrPromptTokens),
		tokensOut: intAttr(attrs, attrOutputTokens, attrCompletionTokens),
	}
	if k, ok := kindByOperation[strAttr(attrs, attrOperationName)]; ok {
		m.kind = k
	}
	attrs.Range(func(key string, _ pcommon.Value) bool {
		if strings.HasPrefix(key, mcpAttrPrefix) {
			m.attrs.MCP = true
			return false
		}
		return true
	})
	if m.attrs.MCP && m.kind == store.KindOther {
		m.kind = store.KindTool
	}
	return m
}

// spanContents gathers conversation and tool io from legacy span events and
// from the current message/tool-call attributes, in that order.
func spanContents(span ptrace.Span) []store.Content {
	var contents []store.Content
	add := func(role, body string) {
		if body == "" {
			return
		}
		contents = append(contents, store.Content{
			SpanID:    span.SpanID().String(),
			Role:      role,
			Seq:       len(contents),
			Body:      body,
			MediaType: mediaType(body),
		})
	}
	events := span.Events()
	for i := 0; i < events.Len(); i++ {
		ev := events.At(i)
		role, ok := roleByEvent[ev.Name()]
		if !ok {
			continue
		}
		body := strAttr(ev.Attributes(), bodyAttrs...)
		if body == "" {
			raw, err := json.Marshal(ev.Attributes().AsRaw())
			if err != nil {
				continue
			}
			body = string(raw)
		}
		add(role, body)
	}
	attrs := span.Attributes()
	for _, m := range parseMessages(strAttr(attrs, attrSystemInstr), "system") {
		add(m.role, m.body)
	}
	for _, m := range parseMessages(strAttr(attrs, attrInputMessages), "user") {
		add(m.role, m.body)
	}
	for _, m := range parseMessages(strAttr(attrs, attrOutputMessages), "assistant") {
		add(m.role, m.body)
	}
	add("input", strAttr(attrs, attrToolArguments))
	add("output", strAttr(attrs, attrToolResult))
	return contents
}

type message struct {
	role string
	body string
}

func parseMessages(raw, defaultRole string) []message {
	if raw == "" {
		return nil
	}
	var elems []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &elems); err != nil {
		return []message{{role: defaultRole, body: raw}}
	}
	msgs := make([]message, 0, len(elems))
	for _, el := range elems {
		var m struct {
			Role  string          `json:"role"`
			Parts json.RawMessage `json:"parts"`
		}
		role, body := defaultRole, string(el)
		if err := json.Unmarshal(el, &m); err == nil {
			if m.Role != "" {
				role = m.Role
			}
			if len(m.Parts) > 0 {
				body = string(m.Parts)
			}
		}
		msgs = append(msgs, message{role: role, body: body})
	}
	return msgs
}

func mediaType(body string) string {
	if json.Valid([]byte(body)) {
		return "application/json"
	}
	return "text/plain"
}

func strAttr(m pcommon.Map, keys ...string) string {
	for _, k := range keys {
		if v, ok := m.Get(k); ok && v.AsString() != "" {
			return v.AsString()
		}
	}
	return ""
}

func intAttr(m pcommon.Map, keys ...string) int64 {
	for _, k := range keys {
		v, ok := m.Get(k)
		if !ok {
			continue
		}
		switch v.Type() {
		case pcommon.ValueTypeInt:
			return v.Int()
		case pcommon.ValueTypeDouble:
			return int64(v.Double())
		case pcommon.ValueTypeStr:
			if n, err := strconv.ParseInt(v.Str(), 10, 64); err == nil {
				return n
			}
		}
	}
	return 0
}
