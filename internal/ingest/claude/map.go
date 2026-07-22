// Package claude tails Claude Code session logs under ~/.claude/projects.
package claude

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

// The log format is unversioned and will break: every line is parsed
// independently, a bad line becomes a parse_error finding, and mapping
// continues.

type line struct {
	Type        string          `json:"type"`
	UUID        string          `json:"uuid"`
	ParentUUID  string          `json:"parentUuid"`
	IsSidechain bool            `json:"isSidechain"`
	Timestamp   time.Time       `json:"timestamp"`
	CWD         string          `json:"cwd"`
	GitBranch   string          `json:"gitBranch"`
	Version     string          `json:"version"`
	RequestID   string          `json:"requestId"`
	Message     json.RawMessage `json:"message"`
	Summary     string          `json:"summary"`
	AITitle     string          `json:"aiTitle"`
}

type message struct {
	ID      string          `json:"id"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"` // string or []block
	Usage   *usage          `json:"usage"`
}

type usage struct {
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadTokens     int64 `json:"cache_read_input_tokens"`
}

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// agentTools spawn subagents whose sidechain lines nest under the tool span.
var agentTools = map[string]bool{"Task": true, "Agent": true, "dispatch_agent": true}

// mapper turns one session file's lines into span/content/finding deltas.
type mapper struct {
	runID       string
	capture     bool
	lineNo      int
	root        store.Span
	tsByUUID    map[string]time.Time
	llmSpans    map[string]*store.Span // by message id
	nextSeq     map[string]int         // per span id
	openTools   map[string]*store.Span // by tool_use id
	openTasks   []string               // open agent-tool span ids, innermost last
	pendingUser map[bool][]string      // buffered user texts by sidechain flag
	lastTS      map[bool]time.Time     // last event time by sidechain flag
}

func newMapper(runID string, captureContent bool) *mapper {
	return &mapper{
		runID:       runID,
		capture:     captureContent,
		tsByUUID:    make(map[string]time.Time),
		llmSpans:    make(map[string]*store.Span),
		nextSeq:     make(map[string]int),
		openTools:   make(map[string]*store.Span),
		pendingUser: make(map[bool][]string),
		lastTS:      make(map[bool]time.Time),
	}
}

// runIDForFile derives the run id from the session file name.
func runIDForFile(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".jsonl")
}

// process maps one raw line into a batch delta and an optional run label.
func (m *mapper) process(raw []byte) (store.Batch, string) {
	m.lineNo++
	b := store.Batch{Source: "claude"}
	var ln line
	if err := json.Unmarshal(raw, &ln); err != nil {
		b.Findings = append(b.Findings, m.parseError(err, raw))
		return b, ""
	}
	if !ln.Timestamp.IsZero() {
		m.tsByUUID[ln.UUID] = ln.Timestamp
		m.lastTS[ln.IsSidechain] = ln.Timestamp
		m.touchRoot(&b, ln)
	}
	switch ln.Type {
	case "summary":
		return b, ln.Summary
	case "ai-title":
		return b, ln.AITitle
	case "user":
		if err := m.userLine(&b, ln); err != nil {
			b.Findings = append(b.Findings, m.parseError(err, raw))
		}
	case "assistant":
		if err := m.assistantLine(&b, ln); err != nil {
			b.Findings = append(b.Findings, m.parseError(err, raw))
		}
	}
	return b, ""
}

func (m *mapper) parseError(err error, raw []byte) store.Finding {
	detail, _ := json.Marshal(map[string]any{
		"line":    m.lineNo,
		"error":   err.Error(),
		"snippet": truncateBytes(raw, 160),
	})
	return store.Finding{
		RunID:    m.runID,
		Type:     "parse_error",
		Severity: "warning",
		Detail:   string(detail),
	}
}

func (m *mapper) rootID() string {
	return m.runID + ":root"
}

// touchRoot keeps the session root span spanning first to last event.
func (m *mapper) touchRoot(b *store.Batch, ln line) {
	if m.root.ID == "" {
		m.root = store.Span{
			ID: m.rootID(), RunID: m.runID, Kind: store.KindAgent, Name: "session",
			StartedAt: ln.Timestamp, Status: "ok",
			Attrs: store.Attrs{Raw: map[string]any{}},
		}
	}
	// Not every line carries the session fields; adopt them when one does.
	if m.root.Name == "session" && ln.CWD != "" {
		m.root.Name = filepath.Base(ln.CWD)
	}
	for k, v := range rawAttrs(ln) {
		m.root.Attrs.Raw[k] = v
	}
	if ln.Timestamp.After(m.root.EndedAt) {
		m.root.EndedAt = ln.Timestamp
	}
	b.Spans = upsertSpan(b.Spans, m.root)
}

func rawAttrs(ln line) map[string]any {
	raw := map[string]any{}
	if ln.CWD != "" {
		raw["cwd"] = ln.CWD
	}
	if ln.GitBranch != "" {
		raw["git_branch"] = ln.GitBranch
	}
	if ln.Version != "" {
		raw["claude_code_version"] = ln.Version
	}
	return raw
}

func (m *mapper) userLine(b *store.Batch, ln line) error {
	var msg message
	if err := json.Unmarshal(ln.Message, &msg); err != nil {
		return fmt.Errorf("user message: %w", err)
	}
	var text string
	if err := json.Unmarshal(msg.Content, &text); err == nil {
		m.pendingUser[ln.IsSidechain] = append(m.pendingUser[ln.IsSidechain], text)
		return nil
	}
	var blocks []block
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return fmt.Errorf("user content: %w", err)
	}
	for _, bl := range blocks {
		switch bl.Type {
		case "text":
			m.pendingUser[ln.IsSidechain] = append(m.pendingUser[ln.IsSidechain], bl.Text)
		case "tool_result":
			m.closeTool(b, bl, ln.Timestamp)
		}
	}
	return nil
}

func (m *mapper) closeTool(b *store.Batch, bl block, ts time.Time) {
	sp, ok := m.openTools[bl.ToolUseID]
	if !ok {
		return // result for a tool call outside the tailed window
	}
	delete(m.openTools, bl.ToolUseID)
	if agentTools[sp.Attrs.ToolName] {
		m.popTask(sp.ID)
	}
	sp.EndedAt = ts
	if bl.IsError {
		sp.Status = "error"
	}
	b.Spans = upsertSpan(b.Spans, *sp)
	// An empty result is still a result: contract analysis needs the row.
	m.addToolOutput(b, sp.ID, blockBody(bl.Content))
}

func (m *mapper) popTask(spanID string) {
	for i := len(m.openTasks) - 1; i >= 0; i-- {
		if m.openTasks[i] == spanID {
			m.openTasks = append(m.openTasks[:i], m.openTasks[i+1:]...)
			return
		}
	}
}

func (m *mapper) assistantLine(b *store.Batch, ln line) error {
	var msg message
	if err := json.Unmarshal(ln.Message, &msg); err != nil {
		return fmt.Errorf("assistant message: %w", err)
	}
	id := msg.ID
	if id == "" {
		id = ln.UUID
	}
	sp, ok := m.llmSpans[id]
	if !ok {
		sp = &store.Span{
			ID: id, RunID: m.runID, ParentID: m.llmParent(ln.IsSidechain),
			Kind: store.KindLLM, Name: "chat " + msg.Model,
			StartedAt: m.llmStart(ln), EndedAt: ln.Timestamp, Status: "ok",
			Attrs: store.Attrs{Model: msg.Model, Provider: "anthropic", Raw: rawAttrs(ln)},
		}
		if ln.RequestID != "" {
			sp.Attrs.Raw["request_id"] = ln.RequestID
		}
		m.llmSpans[id] = sp
		for _, text := range m.pendingUser[ln.IsSidechain] {
			m.addContent(b, sp.ID, "user", text)
		}
		m.pendingUser[ln.IsSidechain] = nil
	}
	if ln.Timestamp.After(sp.EndedAt) {
		sp.EndedAt = ln.Timestamp
	}
	if msg.Usage != nil {
		// Same-message lines repeat the usage struct; take it once, at its max.
		in := msg.Usage.InputTokens + msg.Usage.CacheCreationTokens + msg.Usage.CacheReadTokens
		sp.TokensIn = max(sp.TokensIn, in)
		sp.TokensOut = max(sp.TokensOut, msg.Usage.OutputTokens)
		sp.Attrs.Raw["usage"] = *msg.Usage
	}
	var blocks []block
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return fmt.Errorf("assistant content: %w", err)
	}
	for _, bl := range blocks {
		switch bl.Type {
		case "text":
			m.addContent(b, sp.ID, "assistant", bl.Text)
		case "thinking":
			m.addContent(b, sp.ID, "thinking", bl.Thinking)
		case "tool_use":
			m.openTool(b, sp.ID, bl, ln.Timestamp)
		}
	}
	b.Spans = upsertSpan(b.Spans, *sp)
	return nil
}

func (m *mapper) llmParent(sidechain bool) string {
	if sidechain && len(m.openTasks) > 0 {
		return m.openTasks[len(m.openTasks)-1]
	}
	return m.rootID()
}

// llmStart approximates request start with the parent line's timestamp:
// the previous chain event is when this request became possible.
func (m *mapper) llmStart(ln line) time.Time {
	if ts, ok := m.tsByUUID[ln.ParentUUID]; ok && ts.Before(ln.Timestamp) {
		return ts
	}
	return ln.Timestamp
}

func (m *mapper) openTool(b *store.Batch, parentID string, bl block, ts time.Time) {
	sp := &store.Span{
		ID: bl.ID, RunID: m.runID, ParentID: parentID,
		Kind: store.KindTool, Name: bl.Name,
		StartedAt: ts, Status: "ok",
		Attrs: store.Attrs{
			ToolName: bl.Name,
			MCP:      strings.HasPrefix(bl.Name, "mcp__"),
		},
	}
	m.openTools[bl.ID] = sp
	if agentTools[bl.Name] {
		m.openTasks = append(m.openTasks, sp.ID)
	}
	b.Spans = upsertSpan(b.Spans, *sp)
	if len(bl.Input) > 0 {
		m.addContent(b, sp.ID, "input", string(bl.Input))
	}
}

func (m *mapper) addContent(b *store.Batch, spanID, role, body string) {
	if body == "" {
		return
	}
	m.append(b, spanID, role, body)
}

func (m *mapper) addToolOutput(b *store.Batch, spanID, body string) {
	m.append(b, spanID, "output", body)
}

func (m *mapper) append(b *store.Batch, spanID, role, body string) {
	if !m.capture {
		return
	}
	mediaType := "text/plain"
	if json.Valid([]byte(body)) {
		mediaType = "application/json"
	}
	b.Contents = append(b.Contents, store.Content{
		SpanID: spanID, Role: role, Seq: m.nextSeq[spanID],
		Body: body, MediaType: mediaType,
	})
	m.nextSeq[spanID]++
}

// blockBody renders a tool_result content field: plain string, or raw JSON.
func blockBody(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

// upsertSpan keeps one entry per span id within a delta batch.
func upsertSpan(spans []store.Span, sp store.Span) []store.Span {
	for i := range spans {
		if spans[i].ID == sp.ID {
			spans[i] = sp
			return spans
		}
	}
	return append(spans, sp)
}

func truncateBytes(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
