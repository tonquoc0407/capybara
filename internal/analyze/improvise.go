package analyze

import (
	"context"
	"sort"
	"strings"

	"github.com/tonquoc0407/capybara/internal/store"
)

// Improvise detection is tuned for low false positives: it fires only when
// the next llm turn demonstrably references a failed tool call (by name or by
// quoting its broken output) without acknowledging the failure.

var failureWords = []string{
	"error", "fail", "failed", "failure", "unable", "cannot", "can't",
	"couldn't", "invalid", "empty", "missing", "retry", "not found",
	"no result", "denied", "timeout", "timed out", "crash", "exception",
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
		if f.Type == "malformed" || f.Type == "empty_payload" {
			// A broken payload is a stronger cause than a bare error status.
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
		evidence, err := a.consumedEvidence(ctx, tool, next)
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

// nextLLM finds the llm span that received the failed result: the first llm
// turn after the tool, in the same chain (sibling of the tool or of its
// issuing llm span). Sidechain turns never match main-chain tools.
func (a *Analyzer) nextLLM(rc *runContext, tool store.Span) (store.Span, bool) {
	grandparent := ""
	if p, ok := rc.byID[tool.ParentID]; ok {
		grandparent = p.ParentID
	}
	for _, llm := range rc.llms {
		if !llm.EndedAt.After(tool.EndedAt) || llm.ID == tool.ParentID {
			continue
		}
		if llm.ParentID == tool.ParentID || (grandparent != "" && llm.ParentID == grandparent) {
			return llm, true
		}
	}
	return store.Span{}, false
}

// consumedEvidence returns a description of how the llm output references the
// failed call, or "" when it does not or acknowledges the failure.
func (a *Analyzer) consumedEvidence(ctx context.Context, tool, llm store.Span) (string, error) {
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
	for _, w := range failureWords {
		if strings.Contains(output, w) {
			return "", nil
		}
	}
	if name := strings.ToLower(toolName(tool)); name != "" && strings.Contains(output, name) {
		return "output mentions " + toolName(tool), nil
	}
	toolContents, err := a.st.Contents(ctx, tool.ID)
	if err != nil {
		return "", err
	}
	for _, c := range toolContents {
		if c.Role == "output" && sharesSubstring(strings.ToLower(c.Body), output) {
			return "output quotes the failed result", nil
		}
	}
	return "", nil
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
