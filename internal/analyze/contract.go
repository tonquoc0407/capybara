package analyze

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tonquoc0407/capybara/internal/store"
)

// declaredSchemaAttr is where the SDK attaches a Pydantic schema to a span;
// declared beats learned.
const declaredSchemaAttr = "capybara.schema"

// checkTool validates one completed tool span's output against the tool's
// current schema, learning or widening as it goes. A span without recorded
// output is skipped: analysis never guesses.
func (a *Analyzer) checkTool(ctx context.Context, sp store.Span) ([]store.Finding, error) {
	contents, err := a.st.Contents(ctx, sp.ID)
	if err != nil {
		return nil, err
	}
	var output *store.Content
	for i := range contents {
		if contents[i].Role == "output" {
			output = &contents[i]
		}
	}
	if output == nil {
		return nil, nil
	}
	tool := sp.Attrs.ToolName
	if tool == "" {
		tool = sp.Name
	}
	current, declared, err := a.currentSchema(ctx, sp, tool)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(output.Body) == "" {
		return []store.Finding{finding(sp, "empty_payload", "warning",
			map[string]any{"tool": tool})}, nil
	}
	var value any
	if err := json.Unmarshal([]byte(output.Body), &value); err != nil {
		return a.nonJSONOutput(ctx, sp, tool, output.Body, current, declared)
	}
	reported := reportedError(sp, tool, value)
	obs := infer(value, 0)
	if current == nil {
		return reported, a.learn(ctx, sp, tool, obs)
	}
	d := diffSchemas(current, obs, "")
	if d.breaking() {
		if !declared && rootEncodingFlip(current, obs, d) {
			return reported, a.touch(ctx, sp, tool, mergeSchemas(current, obs))
		}
		if !declared {
			if err := a.adopt(ctx, sp, tool, obs); err != nil {
				return nil, err
			}
		}
		return append(reported, finding(sp, "drift", "warning", map[string]any{
			"tool": tool, "missing": d.Missing, "retyped": d.Retyped,
		})), nil
	}
	if declared {
		return reported, nil
	}
	merged := current
	if d.widened {
		merged = mergeSchemas(current, obs)
	}
	return reported, a.touch(ctx, sp, tool, merged)
}

// reportedError catches the output that announces its own failure. Agent
// frameworks overwhelmingly return an error value rather than raising, so the
// span still ends "ok" and nothing downstream — least of all the improvise
// check — ever learns the call went wrong. Only top-level keys of a JSON object
// are read: scanning free text for the word "error" would flag every search
// result that mentions one.
func reportedError(sp store.Span, tool string, value any) []store.Finding {
	obj, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	reason := ""
	switch {
	case truthy(obj["error"]):
		reason = "error"
	case obj["isError"] == true: // the MCP tool-result convention
		reason = "isError"
	case obj["ok"] == false || obj["success"] == false:
		reason = "not ok"
	case httpFailure(obj["status"]) || httpFailure(obj["status_code"]):
		reason = "status"
	}
	if reason == "" {
		return nil
	}
	return []store.Finding{finding(sp, "tool_error", "warning", map[string]any{
		"tool": tool, "reason": reason,
	})}
}

// truthy treats absent, null, false and the empty string as "no error", so a
// tool that always carries an empty error field is not flagged on every call.
func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return strings.TrimSpace(t) != ""
	case float64:
		return t != 0
	case []any:
		return len(t) > 0
	case map[string]any:
		return len(t) > 0
	}
	return false
}

func httpFailure(v any) bool {
	n, ok := v.(float64)
	return ok && n >= 400 && n < 600
}

// nonJSONOutput handles unparseable bodies: malformed only when the contract
// says JSON and has never accepted text; plain-text tools learn a string schema
// and never drift.
func (a *Analyzer) nonJSONOutput(ctx context.Context, sp store.Span, tool, body string,
	current *jsonSchema, declared bool,
) ([]store.Finding, error) {
	reported := textError(sp, tool, body)
	if current == nil {
		return reported, a.learn(ctx, sp, tool, infer(body, 0))
	}
	if !current.Types.has("string") && hasContainer(current.Types) {
		return []store.Finding{finding(sp, "malformed", "warning", map[string]any{
			"tool": tool, "want": joinTypes(current.Types),
		})}, nil
	}
	if declared {
		return reported, nil
	}
	return reported, a.touch(ctx, sp, tool, current)
}

// errorPrefixes are how a tool that returns text rather than raising announces
// a failure. LangChain's own caught-error format opens with "Error:", and tools
// that wrap a query or a shell command follow the same habit.
var errorPrefixes = []string{
	"error:", "error ", "query error", "traceback (most recent call last)",
	"exception:", "fatal:", "failed:",
}

// textError reads only the opening of the payload. Searching the whole body for
// the word "error" would flag every document that happens to discuss one; a
// tool that has failed says so first.
func textError(sp store.Span, tool, body string) []store.Finding {
	head := strings.ToLower(strings.TrimSpace(body))
	if i := strings.IndexByte(head, '\n'); i >= 0 {
		head = head[:i]
	}
	for _, p := range errorPrefixes {
		if strings.HasPrefix(head, p) {
			return []store.Finding{finding(sp, "tool_error", "warning", map[string]any{
				"tool": tool, "reason": "text",
			})}
		}
	}
	return nil
}

// currentSchema returns the schema to validate against and whether it was
// declared on the span rather than learned.
func (a *Analyzer) currentSchema(ctx context.Context, sp store.Span, tool string) (*jsonSchema, bool, error) {
	if raw, ok := sp.Attrs.Raw[declaredSchemaAttr].(string); ok && raw != "" {
		var s jsonSchema
		if err := json.Unmarshal([]byte(raw), &s); err == nil {
			return &s, true, nil
		}
	}
	ts, err := a.st.LatestToolSchema(ctx, tool)
	if err != nil || ts == nil {
		return nil, false, err
	}
	var s jsonSchema
	if err := json.Unmarshal([]byte(ts.Schema), &s); err != nil {
		return nil, false, fmt.Errorf("stored schema of %s: %w", tool, err)
	}
	a.versions[tool] = ts.Version
	return &s, false, nil
}

func (a *Analyzer) learn(ctx context.Context, sp store.Span, tool string, obs *jsonSchema) error {
	raw, err := json.Marshal(obs)
	if err != nil {
		return fmt.Errorf("marshal schema of %s: %w", tool, err)
	}
	a.versions[tool]++
	return a.st.InsertToolSchema(ctx, store.ToolSchema{
		ToolName: tool, Version: a.versions[tool], Schema: string(raw),
		LearnedFromRun: sp.RunID, FirstSeen: sp.EndedAt, LastSeen: sp.EndedAt,
	})
}

// adopt records the post-drift shape as the new current version.
func (a *Analyzer) adopt(ctx context.Context, sp store.Span, tool string, obs *jsonSchema) error {
	return a.learn(ctx, sp, tool, obs)
}

func (a *Analyzer) touch(ctx context.Context, sp store.Span, tool string, s *jsonSchema) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal schema of %s: %w", tool, err)
	}
	return a.st.TouchToolSchema(ctx, tool, a.versions[tool], string(raw), sp.EndedAt)
}

func finding(sp store.Span, typ, severity string, detail map[string]any) store.Finding {
	raw, _ := json.Marshal(detail)
	return store.Finding{
		RunID: sp.RunID, SpanID: sp.ID, Type: typ, Severity: severity, Detail: string(raw),
	}
}
