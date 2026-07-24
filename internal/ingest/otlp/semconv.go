// Package otlp receives OTLP traces on localhost and maps gen_ai semantic
// conventions onto capybara's internal span model.
package otlp

import (
	"encoding/json"
	"sort"
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

// OpenInference (Arize/Phoenix) and OpenLLMetry (traceloop) predate the gen_ai
// conventions and name most things differently. Neither sets
// gen_ai.operation.name, so without these every span they emit is untyped.
const (
	attrOISpanKind    = "openinference.span.kind"
	attrOIModel       = "llm.model_name"
	attrOIProvider    = "llm.provider"
	attrOISystem      = "llm.system"
	attrOIPromptToks  = "llm.token_count.prompt"
	attrOIOutputToks  = "llm.token_count.completion"
	attrOIToolName    = "tool.name"
	attrOIInput       = "input.value"
	attrOIOutput      = "output.value"
	attrTLSpanKind    = "traceloop.span.kind"
	attrTLEntityName  = "traceloop.entity.name"
	attrTLEntityIn    = "traceloop.entity.input"
	attrTLEntityOut   = "traceloop.entity.output"
	attrTLRequestType = "llm.request.type"
	tlPromptPrefix    = "gen_ai.prompt."
	tlCompletionPre   = "gen_ai.completion."
)

// The Vercel AI SDK's LegacyOpenTelemetry mode. Its current mode emits gen_ai.*
// and needs nothing here, but the legacy names are still what a lot of
// deployed apps send, and they carry no operation name at all.
const (
	attrAIModel      = "ai.model.id"
	attrAIProvider   = "ai.model.provider"
	attrAIInputToks  = "ai.usage.promptTokens"
	attrAIOutputToks = "ai.usage.completionTokens"
	attrAIToolName   = "ai.toolCall.name"
	attrAIToolArgs   = "ai.toolCall.args"
	attrAIToolResult = "ai.toolCall.result"
	attrAIPrompt     = "ai.prompt"
	attrAIResponse   = "ai.response.text"
)

var kindByOISpanKind = map[string]store.Kind{
	"LLM":       store.KindLLM,
	"EMBEDDING": store.KindLLM,
	"TOOL":      store.KindTool,
	"AGENT":     store.KindAgent,
	"CHAIN":     store.KindAgent,
	"RETRIEVER": store.KindRetrieval,
}

var kindByTLSpanKind = map[string]store.Kind{
	"workflow": store.KindAgent,
	"agent":    store.KindAgent,
	"tool":     store.KindTool,
}

var kindByOperation = map[string]store.Kind{
	"invoke_agent":     store.KindAgent,
	"create_agent":     store.KindAgent,
	"invoke_workflow":  store.KindAgent,
	"chat":             store.KindLLM,
	"generate_content": store.KindLLM,
	"text_completion":  store.KindLLM,
	"embeddings":       store.KindLLM,
	"execute_tool":     store.KindTool,
	"retrieval":        store.KindRetrieval,
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
			Model:    strAttr(attrs, attrRequestModel, attrResponseModel, attrOIModel, attrAIModel),
			Provider: strAttr(attrs, attrProviderName, attrSystem, attrOIProvider, attrOISystem, attrAIProvider),
			ToolName: strAttr(attrs, attrToolName, attrMCPToolName, attrOIToolName, attrTLEntityName, attrAIToolName),
		},
		tokensIn:  intAttr(attrs, attrInputTokens, attrPromptTokens, attrOIPromptToks, attrAIInputToks),
		tokensOut: intAttr(attrs, attrOutputTokens, attrCompletionTokens, attrOIOutputToks, attrAIOutputToks),
	}
	m.kind = spanKind(attrs)
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

// spanKind reads whichever convention the emitter speaks. OpenLLMetry marks
// only its wrapper spans; its model calls are recognised by llm.request.type,
// which is the one attribute it always sets on them.
func spanKind(attrs pcommon.Map) store.Kind {
	if k, ok := kindByOperation[strAttr(attrs, attrOperationName)]; ok {
		return k
	}
	if k, ok := kindByOISpanKind[strings.ToUpper(strAttr(attrs, attrOISpanKind))]; ok {
		return k
	}
	if k, ok := kindByTLSpanKind[strings.ToLower(strAttr(attrs, attrTLSpanKind))]; ok {
		return k
	}
	if strAttr(attrs, attrTLRequestType) != "" {
		return store.KindLLM
	}
	// Last resort for emitters that name the model but not the operation, which
	// older OpenLLMetry releases did. Only inference spans carry a request model.
	if strAttr(attrs, attrRequestModel, attrResponseModel) != "" {
		return store.KindLLM
	}
	// The AI SDK's legacy spans name no operation at all, so the shape of what
	// they carry is the only signal: a tool call names a tool, a model call
	// names a model.
	if strAttr(attrs, attrAIToolName) != "" {
		return store.KindTool
	}
	if strAttr(attrs, attrAIModel) != "" {
		return store.KindLLM
	}
	return store.KindOther
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
	for _, m := range indexedMessages(attrs) {
		add(m.role, m.body)
	}
	// OpenInference and OpenLLMetry both carry one blob each way. What they mean
	// depends on the span: a tool's input is arguments, a model's is a prompt.
	inRole, outRole := "input", "output"
	if spanKind(attrs) == store.KindLLM {
		inRole, outRole = "user", "assistant"
	}
	add("input", unwrapEnvelope(strAttr(attrs, attrAIToolArgs)))
	add("output", unwrapEnvelope(strAttr(attrs, attrAIToolResult)))
	add("user", strAttr(attrs, attrAIPrompt))
	add("assistant", strAttr(attrs, attrAIResponse))
	add(inRole, unwrapEnvelope(strAttr(attrs, attrOIInput, attrTLEntityIn)))
	add(outRole, unwrapEnvelope(strAttr(attrs, attrOIOutput, attrTLEntityOut)))
	return contents
}

// unwrapEnvelope digs the tool's own payload out of a framework wrapper.
// LangChain hands OpenLLMetry a serialized ToolMessage rather than the value the
// tool returned, so what arrives on the span is
// {"output":{"lc":1,...,"kwargs":{"content":"<the payload>"}}} and the input is
// the call arguments buried under per-run checkpoint ids. Storing the wrapper
// means every later reader - the learned contract, the error signal, the
// detail pane, the hash that decides whether two calls are identical - is
// looking at the framework instead of the tool. Anything that does not match a
// known wrapper is returned untouched.
func unwrapEnvelope(body string) string {
	var obj map[string]any
	if body == "" || json.Unmarshal([]byte(body), &obj) != nil {
		return body
	}
	if s, ok := obj["input_str"].(string); ok {
		return s
	}
	inner, ok := obj["output"]
	if !ok {
		return body
	}
	if nested, ok := inner.(map[string]any); ok {
		if _, serialized := nested["lc"]; serialized {
			if kwargs, ok := nested["kwargs"].(map[string]any); ok {
				if content, ok := kwargs["content"].(string); ok {
					return content
				}
			}
		}
	}
	if s, ok := inner.(string); ok {
		return s
	}
	raw, err := json.Marshal(inner)
	if err != nil {
		return body
	}
	return string(raw)
}

// indexedMessages reads OpenLLMetry's flattened conversation, which arrives as
// gen_ai.prompt.0.role, gen_ai.prompt.0.content and so on rather than as one
// serialized list.
func indexedMessages(attrs pcommon.Map) []message {
	bodies := map[string]string{}
	roles := map[string]string{}
	attrs.Range(func(key string, v pcommon.Value) bool {
		for _, prefix := range []string{tlPromptPrefix, tlCompletionPre} {
			rest, ok := strings.CutPrefix(key, prefix)
			if !ok {
				continue
			}
			idx, field, ok := strings.Cut(rest, ".")
			if !ok {
				continue
			}
			switch field {
			case "content":
				bodies[prefix+idx] = v.AsString()
			case "role":
				roles[prefix+idx] = v.AsString()
			}
		}
		return true
	})
	keys := make([]string, 0, len(bodies))
	for k := range bodies {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	msgs := make([]message, 0, len(keys))
	for _, k := range keys {
		role := roles[k]
		if role == "" {
			role = "user"
			if strings.HasPrefix(k, tlCompletionPre) {
				role = "assistant"
			}
		}
		msgs = append(msgs, message{role: role, body: bodies[k]})
	}
	return msgs
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
