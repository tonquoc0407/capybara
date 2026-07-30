package analyze

import (
	"context"
	"strings"

	"github.com/tonquoc0407/capybara/internal/store"
)

// Prompt-injection detection targets the indirect case (OWASP LLM01, the top
// gen-AI risk): a tool or retrieval span returns attacker-authored instructions
// through a channel the model implicitly trusts, and a downstream llm turn
// ingests them. It flags the source span when its output carries an injection
// directive that an llm then consumed - the realised risk, not a payload that
// reached no model. Markers are specific multi-word imperatives, so benign tool
// data and JSON keys do not trip it; the list favours precision over reach and
// is meant to grow against a labelled corpus.
var injectionMarkers = []string{
	"ignore previous instructions",
	"ignore all previous",
	"ignore the above instructions",
	"ignore your instructions",
	"ignore prior instructions",
	"disregard previous instructions",
	"disregard the above instructions",
	"disregard all previous",
	"disregard your instructions",
	"forget previous instructions",
	"forget all previous instructions",
	"reveal your system prompt",
	"print your system prompt",
	"repeat the words above",
	"repeat the text above",
	"do not tell the user",
	"don't tell the user",
	"without telling the user",
	"pretend you are",
}

func (a *Analyzer) injectionRun(ctx context.Context, rc *runContext) ([]store.Finding, error) {
	var findings []store.Finding
	for _, sp := range rc.spans {
		if sp.Kind != store.KindTool && sp.Kind != store.KindRetrieval {
			continue
		}
		consumer, ok := nextLLMConsumer(sp, rc.llms, rc.byID)
		if !ok || (!rc.fresh[sp.ID] && !rc.fresh[consumer.ID]) {
			continue
		}
		marker, err := a.outputInjection(ctx, sp)
		if err != nil {
			return nil, err
		}
		if marker == "" {
			continue
		}
		findings = append(findings, finding(sp, "prompt_injection", "warning", map[string]any{
			"tool":          toolName(sp),
			"consumer_span": consumer.ID,
			"evidence":      marker,
		}))
	}
	return findings, nil
}

func (a *Analyzer) outputInjection(ctx context.Context, sp store.Span) (string, error) {
	contents, err := a.st.Contents(ctx, sp.ID)
	if err != nil {
		return "", err
	}
	for _, c := range contents {
		if c.Role != "output" {
			continue
		}
		body := strings.ToLower(c.Body)
		for _, m := range injectionMarkers {
			if strings.Contains(body, m) {
				return m, nil
			}
		}
	}
	return "", nil
}
