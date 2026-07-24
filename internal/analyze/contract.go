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
	obs := infer(value, 0)
	if current == nil {
		return nil, a.learn(ctx, sp, tool, obs)
	}
	d := diffSchemas(current, obs, "")
	if d.breaking() {
		if !declared && rootEncodingFlip(current, obs, d) {
			return nil, a.touch(ctx, sp, tool, mergeSchemas(current, obs))
		}
		if !declared {
			if err := a.adopt(ctx, sp, tool, obs); err != nil {
				return nil, err
			}
		}
		return []store.Finding{finding(sp, "drift", "warning", map[string]any{
			"tool": tool, "missing": d.Missing, "retyped": d.Retyped,
		})}, nil
	}
	if declared {
		return nil, nil
	}
	merged := current
	if d.widened {
		merged = mergeSchemas(current, obs)
	}
	return nil, a.touch(ctx, sp, tool, merged)
}

// nonJSONOutput handles unparseable bodies: malformed only when the contract
// says JSON and has never accepted text; plain-text tools learn a string schema
// and never drift.
func (a *Analyzer) nonJSONOutput(ctx context.Context, sp store.Span, tool, body string,
	current *jsonSchema, declared bool,
) ([]store.Finding, error) {
	if current == nil {
		return nil, a.learn(ctx, sp, tool, infer(body, 0))
	}
	if !current.Types.has("string") && hasContainer(current.Types) {
		return []store.Finding{finding(sp, "malformed", "warning", map[string]any{
			"tool": tool, "want": joinTypes(current.Types),
		})}, nil
	}
	if declared {
		return nil, nil
	}
	return nil, a.touch(ctx, sp, tool, current)
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
