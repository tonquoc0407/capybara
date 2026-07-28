package analyze

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tonquoc0407/capybara/internal/store"
)

// Improvise detection is tuned for low false positives. It fires when the turn
// that consumed a failed tool call answered past the failure without
// acknowledging it — either demonstrably referencing the call (naming the tool
// or quoting its broken output), or committing to a final answer: the model's
// last word in its agent scope, with no further tool call to recover. A model
// still working produces a later turn, so the terminal test is what separates a
// fabricated answer from an agent mid-recovery. Acknowledgment is read two ways
// — calling the tool again, which needs no language at all, and saying so, which
// needs a word list per language the agent answers in.

var failureWords = []string{
	"error", "fail", "failed", "failure", "unable", "cannot", "can't",
	"couldn't", "invalid", "empty", "missing", "retry", "not found",
	"no result", "denied", "timeout", "timed out", "crash", "exception",
	"lỗi", "thất bại", "không thể", "không tìm thấy", "không có kết quả",
	"báo lỗi", "thử lại", "hết thời gian", "bị từ chối", "trống",
}

const overlapLen = 12

type runContext struct {
	spans    []store.Span
	byID     map[string]store.Span
	llms     []store.Span // completed llm spans, chronological
	failType map[string]string
	fresh    map[string]bool // span ids in this sweep
}

func newRunContext(spans []store.Span, findings []store.Finding, fresh map[string]bool) *runContext {
	rc := &runContext{
		byID:     make(map[string]store.Span, len(spans)),
		failType: make(map[string]string),
		fresh:    fresh,
		spans:    spans,
	}
	for _, sp := range spans {
		rc.byID[sp.ID] = sp
		if sp.Kind == store.KindLLM && !sp.EndedAt.IsZero() {
			rc.llms = append(rc.llms, sp)
		}
		if sp.Kind == store.KindTool && sp.Status == "error" {
			rc.failType[sp.ID] = "error"
		}
	}
	for _, f := range findings {
		if f.SpanID == "" {
			continue
		}
		switch f.Type {
		case "malformed", "empty_payload", "tool_error":
			// A payload that is broken, empty, or says so itself is a stronger
			// cause than a bare error status.
			rc.failType[f.SpanID] = f.Type
		}
	}
	sort.Slice(rc.llms, func(i, j int) bool {
		if !rc.llms[i].EndedAt.Equal(rc.llms[j].EndedAt) {
			return rc.llms[i].EndedAt.Before(rc.llms[j].EndedAt)
		}
		return rc.llms[i].ID < rc.llms[j].ID
	})
	return rc
}

// improviseRun evaluates every failed tool span whose verdict may have
// changed this sweep: the tool is new, or its follow-up llm span is.
func (a *Analyzer) improviseRun(ctx context.Context, rc *runContext) ([]store.Finding, error) {
	var findings []store.Finding
	for toolID, cause := range rc.failType {
		tool, ok := rc.byID[toolID]
		if !ok || tool.EndedAt.IsZero() {
			continue
		}
		next, ok := a.nextLLM(rc, tool)
		if !ok || (!rc.fresh[toolID] && !rc.fresh[next.ID]) {
			continue
		}
		evidence, err := a.consumedEvidence(ctx, rc, tool, next)
		if err != nil {
			return nil, err
		}
		if evidence == "" {
			continue
		}
		findings = append(findings, finding(next, "improvised", "error", map[string]any{
			"tool":      toolName(tool),
			"tool_span": tool.ID,
			"cause":     cause,
			"evidence":  evidence,
		}))
	}
	return findings, nil
}

func (a *Analyzer) nextLLM(rc *runContext, tool store.Span) (store.Span, bool) {
	return nextLLMConsumer(tool, rc.llms, rc.byID)
}

// consumedEvidence returns a description of how the llm output references the
// failed call, or "" when it does not or when the turn shows it noticed the
// failure.
func (a *Analyzer) consumedEvidence(ctx context.Context, rc *runContext, tool, llm store.Span) (string, error) {
	llmContents, err := a.st.Contents(ctx, llm.ID)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for _, c := range llmContents {
		if c.Role == "assistant" {
			out.WriteString(c.Body)
			out.WriteString("\n")
		}
	}
	output := strings.ToLower(out.String())
	if strings.TrimSpace(output) == "" {
		return "", nil
	}
	name := toolName(tool)
	if retried(rc, llm, name) {
		return "", nil
	}
	for _, w := range failureWords {
		if strings.Contains(output, w) {
			return "", nil
		}
	}
	if name != "" && strings.Contains(output, strings.ToLower(name)) {
		return "output mentions " + name, nil
	}
	toolContents, err := a.st.Contents(ctx, tool.ID)
	if err != nil {
		return "", err
	}
	var input string
	for _, c := range toolContents {
		if c.Role == "output" && sharesSubstring(strings.ToLower(c.Body), output) {
			return "output quotes the failed result", nil
		}
		if c.Role == "input" {
			input = c.Body
		}
	}
	// A model that neither named the tool nor quoted it can still have answered
	// past its failure — real fabrications rarely cite the function that failed.
	// Two guards keep this from firing on an agent that recovered or moved on:
	// the turn must be terminal, and it must commit to the answer the failed call
	// was supposed to supply. That commitment shows two ways: echoing a value from
	// the call's input, or being itself a bare value — the number the tool would
	// have returned, standing alone where a sign-off or system notice never is.
	if terminalTurn(rc, llm) && (responsive(input, output) || bareValue(output)) {
		return "committed to an answer after the failure", nil
	}
	return "", nil
}

// terminalTurn reports whether no later llm turn shares this turn's agent scope:
// the model's last word there, with nothing after it to walk the failure back.
func terminalTurn(rc *runContext, llm store.Span) bool {
	scope := agentScope(llm, rc.byID)
	for _, other := range rc.llms {
		if other.ID != llm.ID && other.EndedAt.After(llm.EndedAt) &&
			agentScope(other, rc.byID) == scope {
			return false
		}
	}
	return true
}

// responsive reports whether the answer names the subject of the failed call —
// a value token from the tool's input, not a structural key. It is the
// difference between "NVDA is worth $875" after a failed quote and an unrelated
// goodbye after a failed shell command.
func responsive(input, output string) bool {
	for tok := range inputValueTokens(input) {
		if strings.Contains(output, tok) {
			return true
		}
	}
	return false
}

// inputValueTokens returns the lowercased word tokens of an input's string
// values, four runes or longer. JSON keys are skipped so a match is on what was
// looked up, not on the argument names every call to a tool shares.
func inputValueTokens(input string) map[string]struct{} {
	toks := map[string]struct{}{}
	emit := func(s string) {
		for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}) {
			if utf8.RuneCountInString(f) >= 4 {
				toks[f] = struct{}{}
			}
		}
	}
	var v any
	if json.Unmarshal([]byte(input), &v) == nil {
		collectValues(v, emit)
	} else {
		emit(input)
	}
	return toks
}

// bareValue reports whether the answer is dominated by digits: a number the
// tool would have returned, delivered without a sentence to hedge it. A price
// like "$242.50" passes; a goodbye has no digit and a session notice drowns its
// clock reading in letters, so both fail. Currency and thousands punctuation do
// not count against it.
func bareValue(output string) bool {
	var digits, letters int
	for _, r := range output {
		switch {
		case unicode.IsDigit(r):
			digits++
		case unicode.IsLetter(r):
			letters++
		}
	}
	return digits >= 2 && letters <= digits
}

func collectValues(v any, emit func(string)) {
	switch t := v.(type) {
	case string:
		emit(t)
	case []any:
		for _, e := range t {
			collectValues(e, emit)
		}
	case map[string]any:
		for _, e := range t {
			collectValues(e, emit)
		}
	}
}

// retried reports whether the turn that consumed the failure called the same
// tool again, the language-free sign that it noticed.
func retried(rc *runContext, llm store.Span, name string) bool {
	for _, sp := range rc.spans {
		if sp.Kind == store.KindTool && sp.ParentID == llm.ID && toolName(sp) == name {
			return true
		}
	}
	return false
}

// sharesSubstring reports whether any overlapLen-sized window of a occurs in b.
func sharesSubstring(a, b string) bool {
	const limit = 4096
	if len(a) > limit {
		a = a[:limit]
	}
	if len(a) < overlapLen {
		return a != "" && strings.Contains(b, a)
	}
	for i := 0; i+overlapLen <= len(a); i += overlapLen / 2 {
		if strings.Contains(b, a[i:i+overlapLen]) {
			return true
		}
	}
	return false
}

func toolName(sp store.Span) string {
	if sp.Attrs.ToolName != "" {
		return sp.Attrs.ToolName
	}
	return sp.Name
}
