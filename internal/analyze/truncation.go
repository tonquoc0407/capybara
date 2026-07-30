package analyze

import (
	"context"
	"strings"

	"github.com/tonquoc0407/capybara/internal/store"
)

// Truncation detection flags a final answer the model did not finish: its turn
// stopped because it hit the token limit, not because it was done. The signal is
// a finish or stop reason of "length"/"max_tokens" on the terminal llm turn -
// the model's last word, cut off and presented as if complete. Reasons are read
// from raw attributes across conventions, since each names the field its own way.
var truncationReasons = map[string]bool{
	"length":            true,
	"max_tokens":        true,
	"maxtokens":         true,
	"max_output_tokens": true,
	"model_length":      true,
}

func (a *Analyzer) truncationRun(_ context.Context, rc *runContext) []store.Finding {
	var findings []store.Finding
	for _, sp := range rc.spans {
		if sp.Kind != store.KindLLM || !rc.fresh[sp.ID] {
			continue
		}
		if truncatedReason(sp.Attrs.Raw) && terminalTurn(rc, sp) {
			findings = append(findings, finding(sp, "truncated", "warning", map[string]any{}))
		}
	}
	return findings
}

// truncatedReason reports whether any finish or stop reason attribute carries a
// value that means the token limit was hit.
func truncatedReason(raw map[string]any) bool {
	for k, v := range raw {
		lk := strings.ToLower(k)
		if !strings.Contains(lk, "finish_reason") && !strings.Contains(lk, "finishreason") &&
			!strings.Contains(lk, "stop_reason") {
			continue
		}
		if valueMeansTruncated(v) {
			return true
		}
	}
	return false
}

func valueMeansTruncated(v any) bool {
	switch t := v.(type) {
	case string:
		return truncationReasons[strings.ToLower(t)]
	case []any:
		for _, e := range t {
			if valueMeansTruncated(e) {
				return true
			}
		}
	}
	return false
}
