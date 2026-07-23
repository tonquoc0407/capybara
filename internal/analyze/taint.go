package analyze

import (
	"encoding/json"
	"sort"

	"github.com/tonquoc0407/capybara/internal/store"
)

// Taint propagation follows the data path forward from every finding-marked
// span: a bad tool output reaches the llm turn that consumed it, that turn's
// output reaches the next turn, and the last turn reaches the agent's answer.
// The recorded edges let blame walk the final output back to its source.

// taintRun computes a run's taint edges. Pure over the run's spans and findings.
func taintRun(spans []store.Span, findings []store.Finding) []store.Taint {
	if len(spans) == 0 {
		return nil
	}
	runID := spans[0].RunID
	byID := make(map[string]store.Span, len(spans))
	var llms []store.Span
	for _, sp := range spans {
		byID[sp.ID] = sp
		if sp.Kind == store.KindLLM && !sp.EndedAt.IsZero() {
			llms = append(llms, sp)
		}
	}
	sortSpansByEnd(llms)
	origins := taintOrigins(byID, findings)
	feeds := func(a store.Span) (string, bool) {
		switch a.Kind {
		case store.KindTool:
			if l, ok := nextLLMConsumer(a, llms, byID); ok {
				return l.ID, true
			}
		case store.KindLLM:
			if l, ok := nextLLMTurn(a, llms); ok {
				return l.ID, true
			}
			return agentAncestor(a, byID)
		}
		return "", false
	}
	tainted := make(map[string]bool, len(origins))
	queue := make([]string, 0, len(origins))
	for id := range origins {
		tainted[id] = true
		queue = append(queue, id)
	}
	sort.Strings(queue) // deterministic edge order for finding-free comparisons
	seen := make(map[[2]string]bool)
	var edges []store.Taint
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		target, ok := feeds(byID[id])
		if !ok {
			continue
		}
		key := [2]string{target, id}
		if !seen[key] {
			seen[key] = true
			edges = append(edges, store.Taint{RunID: runID, SpanID: target, SourceSpanID: id})
		}
		if !tainted[target] {
			tainted[target] = true
			queue = append(queue, target)
		}
	}
	return edges
}

// taintOrigins are the roots taint flows from: every finding-marked span plus,
// for an improvise finding, the failed tool it blames (which may only carry an
// error status, not a finding of its own).
func taintOrigins(byID map[string]store.Span, findings []store.Finding) map[string]bool {
	origins := make(map[string]bool)
	for _, f := range findings {
		if f.SpanID != "" {
			if _, ok := byID[f.SpanID]; ok {
				origins[f.SpanID] = true
			}
		}
		if f.Type == "improvised" {
			var d struct {
				ToolSpan string `json:"tool_span"`
			}
			if json.Unmarshal([]byte(f.Detail), &d) == nil && d.ToolSpan != "" {
				if _, ok := byID[d.ToolSpan]; ok {
					origins[d.ToolSpan] = true
				}
			}
		}
	}
	return origins
}

// nextLLMConsumer finds the llm span that received a tool's result: the first
// llm turn after the tool, in the same chain (sibling of the tool or of its
// issuing llm span). Sidechain turns never match main-chain tools.
func nextLLMConsumer(tool store.Span, llms []store.Span, byID map[string]store.Span) (store.Span, bool) {
	grandparent := ""
	if p, ok := byID[tool.ParentID]; ok {
		grandparent = p.ParentID
	}
	for _, llm := range llms {
		if !llm.EndedAt.After(tool.EndedAt) || llm.ID == tool.ParentID {
			continue
		}
		if llm.ParentID == tool.ParentID || (grandparent != "" && llm.ParentID == grandparent) {
			return llm, true
		}
	}
	return store.Span{}, false
}

// nextLLMTurn is the next conversation turn after llm in the same chain, whose
// input carries llm's output.
func nextLLMTurn(llm store.Span, llms []store.Span) (store.Span, bool) {
	for _, next := range llms {
		if next.ID == llm.ID || next.ParentID != llm.ParentID {
			continue
		}
		if next.EndedAt.After(llm.EndedAt) {
			return next, true
		}
	}
	return store.Span{}, false
}

// agentAncestor is the nearest enclosing agent span, whose output is the run's
// answer once the last turn feeds it.
func agentAncestor(sp store.Span, byID map[string]store.Span) (string, bool) {
	for id := sp.ParentID; id != ""; {
		parent, ok := byID[id]
		if !ok {
			return "", false
		}
		if parent.Kind == store.KindAgent {
			return parent.ID, true
		}
		id = parent.ParentID
	}
	return "", false
}
