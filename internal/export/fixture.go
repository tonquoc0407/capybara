// Package export turns a recorded run into regression artifacts: a pytest case
// that replays it and a golden fixture for CI.
package export

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/replay"
	"github.com/tonquoc0407/capybara/internal/store"
)

// DefaultDir is where exports land, a directory pytest discovers by default.
const DefaultDir = "capybara_tests"

// Fixture is the recorded input a generated test and a golden snapshot share:
// every completed tool call with its effective contract and the findings a
// regression must not reintroduce.
type Fixture struct {
	Run    string        `json:"run"`
	Span   string        `json:"span,omitempty"`
	Source string        `json:"source"`
	Tools  []ToolFixture `json:"tools"`
}

// ToolFixture carries the fields the replay runner needs to serve a tool
// (hash, output) plus what an assertion needs (input, schema, findings).
type ToolFixture struct {
	Hash     string           `json:"hash"`
	SpanID   string           `json:"span_id"`
	Tool     string           `json:"tool"`
	Input    string           `json:"input"`
	Output   string           `json:"output"`
	Schema   json.RawMessage  `json:"schema"`
	Findings []FindingFixture `json:"findings,omitempty"`
}

// FindingFixture is the shape of a finding a test asserts is gone.
type FindingFixture struct {
	Type    string    `json:"type"`
	Missing []string  `json:"missing,omitempty"`
	Retyped []Retyped `json:"retyped,omitempty"`
}

// Retyped names a field that drifted to a wrong type.
type Retyped struct {
	Field string `json:"field"`
	Want  string `json:"want"`
}

// BuildFixture gathers a run's completed tool calls with their effective
// schemas and findings.
func BuildFixture(ctx context.Context, st *store.Store, runID string) (Fixture, error) {
	return buildFixture(ctx, st, runID, "")
}

// BuildSpanFixture narrows a run's fixture to one tool span, so a test can
// pin the single call a finding came from.
func BuildSpanFixture(ctx context.Context, st *store.Store, runID, spanID string) (Fixture, error) {
	fx, err := buildFixture(ctx, st, runID, spanID)
	if err != nil {
		return Fixture{}, err
	}
	if len(fx.Tools) == 0 {
		return Fixture{}, fmt.Errorf("span %s: no recorded tool call to export", spanID)
	}
	return fx, nil
}

func buildFixture(ctx context.Context, st *store.Store, runID, spanID string) (Fixture, error) {
	spans, err := st.Spans(ctx, runID)
	if err != nil {
		return Fixture{}, err
	}
	contents, err := st.ContentsForRun(ctx, runID)
	if err != nil {
		return Fixture{}, err
	}
	findings, err := st.Findings(ctx, runID)
	if err != nil {
		return Fixture{}, err
	}
	bySpan := make(map[string][]store.Finding)
	for _, f := range findings {
		if f.SpanID != "" {
			bySpan[f.SpanID] = append(bySpan[f.SpanID], f)
		}
	}
	fx := Fixture{Run: runID, Span: spanID, Source: runSource(ctx, st, runID)}
	for _, sp := range spans {
		if sp.Kind != store.KindTool || sp.EndedAt.IsZero() {
			continue
		}
		if spanID != "" && sp.ID != spanID {
			continue
		}
		input, output, ok := toolIO(contents[sp.ID])
		if !ok {
			continue
		}
		tool := sp.Attrs.ToolName
		if tool == "" {
			tool = sp.Name
		}
		tf := ToolFixture{
			Hash:   replay.HashToolCall(tool, input),
			SpanID: sp.ID,
			Tool:   tool,
			Input:  input,
			Output: output,
			Schema: effectiveSchema(ctx, st, sp, tool, output),
		}
		for _, f := range bySpan[sp.ID] {
			if ff, ok := findingFixture(f); ok {
				tf.Findings = append(tf.Findings, ff)
			}
		}
		fx.Tools = append(fx.Tools, tf)
	}
	return fx, nil
}

func toolIO(contents []store.Content) (input, output string, ok bool) {
	for _, c := range contents {
		switch c.Role {
		case "input":
			input = c.Body
		case "output":
			output, ok = c.Body, true
		}
	}
	return input, output, ok
}

// effectiveSchema is the contract a test asserts against: the declared schema,
// then the learned one, then the shape of this output when neither exists.
func effectiveSchema(ctx context.Context, st *store.Store, sp store.Span, tool, output string) json.RawMessage {
	if raw, ok := sp.Attrs.Raw["capybara.schema"].(string); ok && raw != "" {
		return json.RawMessage(raw)
	}
	if ts, err := st.LatestToolSchema(ctx, tool); err == nil && ts != nil {
		return json.RawMessage(ts.Schema)
	}
	return analyze.InferSchema(output)
}

func findingFixture(f store.Finding) (FindingFixture, bool) {
	switch f.Type {
	case "drift":
		var d struct {
			Missing []string `json:"missing"`
			Retyped []struct {
				Field string `json:"field"`
				Want  string `json:"want"`
			} `json:"retyped"`
		}
		_ = json.Unmarshal([]byte(f.Detail), &d)
		ff := FindingFixture{Type: "drift", Missing: d.Missing}
		for _, r := range d.Retyped {
			ff.Retyped = append(ff.Retyped, Retyped{Field: r.Field, Want: r.Want})
		}
		return ff, true
	case "malformed", "empty_payload":
		return FindingFixture{Type: f.Type}, true
	}
	return FindingFixture{}, false
}

func runSource(ctx context.Context, st *store.Store, runID string) string {
	runs, err := st.ListRuns(ctx)
	if err != nil {
		return ""
	}
	for _, r := range runs {
		if r.ID == runID {
			return r.Source
		}
	}
	return ""
}

// WriteGolden snapshots a known-good run as a CI fixture and returns its path.
func WriteGolden(dir string, fx Fixture) (string, error) {
	if err := ensureDir(dir); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "golden_"+shortID(fx.Run)+".json")
	if err := writeJSON(path, fx); err != nil {
		return "", err
	}
	return path, nil
}

func ensureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	return nil
}

func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	return writeFile(path, string(raw)+"\n")
}

func writeFile(path, content string) error {
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// fixtureBase keeps a span export from overwriting its run's export.
func fixtureBase(fx Fixture) string {
	if fx.Span == "" {
		return shortID(fx.Run)
	}
	return shortID(fx.Run) + "_" + shortID(fx.Span)
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
