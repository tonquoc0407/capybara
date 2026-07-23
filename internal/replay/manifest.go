package replay

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/tonquoc0407/capybara/internal/store"
)

// Attributes the SDK records on every span so a run can be re-executed.
const (
	entrypointAttr = "capybara.entrypoint"
	cwdAttr        = "capybara.cwd"
)

// Manifest is the complete instruction set handed to the SDK runner: what to
// execute, what to serve it, and which recorded value the user replaced.
type Manifest struct {
	Version     int         `json:"version"`
	RunID       string      `json:"run_id"`
	ParentRunID string      `json:"parent_run_id"`
	StartSpanID string      `json:"start_span_id,omitempty"`
	Endpoint    string      `json:"endpoint"`
	Entrypoint  []string    `json:"entrypoint"`
	Cwd         string      `json:"cwd"`
	LLM         []LLMEntry  `json:"llm"`
	Tools       []ToolEntry `json:"tools"`
}

// LLMEntry is one cached model response keyed by its request hash.
type LLMEntry struct {
	Hash     string `json:"hash"`
	SpanID   string `json:"span_id"`
	Model    string `json:"model"`
	Response string `json:"response"`
}

// ToolEntry is one recorded tool output keyed by name and arguments. Edited
// marks the span the user changed: the replay's divergence point.
type ToolEntry struct {
	Hash   string `json:"hash"`
	SpanID string `json:"span_id"`
	Tool   string `json:"tool"`
	Output string `json:"output"`
	Edited bool   `json:"edited,omitempty"`
}

// Build assembles a manifest that re-executes parentRunID, substituting
// override for the recorded output of startSpanID when one is given.
func Build(ctx context.Context, st *store.Store, parentRunID, startSpanID, override string) (Manifest, error) {
	if _, err := BuildCache(ctx, st, parentRunID); err != nil {
		return Manifest{}, err
	}
	spans, err := st.Spans(ctx, parentRunID)
	if err != nil {
		return Manifest{}, err
	}
	contents, err := st.ContentsForRun(ctx, parentRunID)
	if err != nil {
		return Manifest{}, err
	}
	entrypoint, cwd, err := entrypointOf(spans)
	if err != nil {
		return Manifest{}, err
	}
	runID, err := newRunID()
	if err != nil {
		return Manifest{}, err
	}
	m := Manifest{
		Version:     1,
		RunID:       runID,
		ParentRunID: parentRunID,
		StartSpanID: startSpanID,
		Entrypoint:  entrypoint,
		Cwd:         cwd,
	}
	cached, err := st.LLMCache(ctx, parentRunID)
	if err != nil {
		return Manifest{}, err
	}
	byLLMSpan := make(map[string]store.CachedLLM, len(cached))
	for _, c := range cached {
		byLLMSpan[c.SpanID] = c
	}
	for _, sp := range spans {
		switch sp.Kind {
		case store.KindLLM:
			if c, ok := byLLMSpan[sp.ID]; ok {
				m.LLM = append(m.LLM, LLMEntry{
					Hash: c.RequestHash, SpanID: sp.ID,
					Model: sp.Attrs.Model, Response: c.Response,
				})
			}
		case store.KindTool:
			entry, ok := toolEntry(sp, contents[sp.ID])
			if !ok {
				continue
			}
			if sp.ID == startSpanID && override != "" {
				entry.Output, entry.Edited = override, true
			}
			m.Tools = append(m.Tools, entry)
		}
	}
	if startSpanID != "" && override != "" && !hasEdit(m.Tools) {
		return Manifest{}, fmt.Errorf("span %s has no recorded tool output to replace", startSpanID)
	}
	return m, nil
}

func toolEntry(sp store.Span, contents []store.Content) (ToolEntry, bool) {
	tool := sp.Attrs.ToolName
	if tool == "" {
		tool = sp.Name
	}
	var input, output string
	haveOutput := false
	for _, c := range contents {
		switch c.Role {
		case "input":
			input = c.Body
		case "output":
			output, haveOutput = c.Body, true
		}
	}
	if !haveOutput {
		return ToolEntry{}, false
	}
	return ToolEntry{
		Hash:   HashToolCall(tool, input),
		SpanID: sp.ID,
		Tool:   tool,
		Output: output,
	}, true
}

func hasEdit(tools []ToolEntry) bool {
	for _, t := range tools {
		if t.Edited {
			return true
		}
	}
	return false
}

// entrypointOf recovers how the recorded process was launched, which only
// runs instrumented with capybara-sdk carry.
func entrypointOf(spans []store.Span) ([]string, string, error) {
	for _, sp := range spans {
		raw, ok := sp.Attrs.Raw[entrypointAttr].(string)
		if !ok || raw == "" {
			continue
		}
		var argv []string
		if err := json.Unmarshal([]byte(raw), &argv); err != nil || len(argv) == 0 {
			continue
		}
		cwd, _ := sp.Attrs.Raw[cwdAttr].(string)
		return argv, cwd, nil
	}
	return nil, "", fmt.Errorf("no recorded entrypoint: replay needs a run traced by capybara.init()")
}

func newRunID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("new run id: %w", err)
	}
	return hex.EncodeToString(id[:]), nil
}
