package analyze

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/tonquoc0407/capybara/internal/store"
)

// Loop detection: identical (tool, input) n-grams repeated back to back.
// Distinct inputs never count as a loop; a Read over ten files is a plan,
// the same Read ten times is a loop.
const (
	maxGram    = 3
	minRepeats = 3
	minCalls   = 4
)

// spike detection: token burn beyond a rolling per-run baseline. The floor is
// in tokens billed as new, which cache reads no longer inflate; measured over
// real Claude history it keeps the warning at ~1% of turns.
const (
	spikeWindow = 5
	spikeFactor = 3.0
	spikeFloor  = 20000
)

func loopFindings(runID string, calls []store.ToolCall) []store.Finding {
	keys := make([]string, len(calls))
	for i, c := range calls {
		sum := sha256.Sum256([]byte(c.Input))
		keys[i] = c.Tool + "\x00" + hex.EncodeToString(sum[:8])
	}
	var findings []store.Finding
	covered := make([]bool, len(calls))
	for n := 1; n <= maxGram; n++ {
		for i := 0; i+n <= len(keys); {
			if covered[i] {
				i++
				continue
			}
			repeats := 1
			for j := i + n; j+n <= len(keys)+1 && equalGram(keys, i, j, n); j += n {
				repeats++
			}
			if repeats < minRepeats || repeats*n < minCalls {
				i++
				continue
			}
			for k := i; k < i+repeats*n; k++ {
				covered[k] = true
			}
			pattern := make([]string, n)
			for k := range n {
				pattern[k] = calls[i+k].Tool
			}
			detail, _ := json.Marshal(map[string]any{"pattern": pattern, "n": n})
			findings = append(findings, store.Finding{
				RunID: runID, SpanID: calls[i].SpanID, Type: "loop",
				Severity: "warning", Detail: string(detail),
			})
			i += repeats * n
		}
	}
	return findings
}

func equalGram(keys []string, a, b, n int) bool {
	if b+n > len(keys) {
		return false
	}
	for k := range n {
		if keys[a+k] != keys[b+k] {
			return false
		}
	}
	return true
}

// spikeFindings flags llm turns burning tokens far beyond the run's recent
// baseline. spans must be one run's spans; order does not matter.
func spikeFindings(spans []store.Span) []store.Finding {
	var llms []store.Span
	for _, sp := range spans {
		if sp.Kind == store.KindLLM && !sp.EndedAt.IsZero() {
			llms = append(llms, sp)
		}
	}
	sortSpansByEnd(llms)
	var findings []store.Finding
	for i, sp := range llms {
		if i < minRepeats {
			continue
		}
		start := max(0, i-spikeWindow)
		var sum int64
		for _, prev := range llms[start:i] {
			sum += prev.TokensIn + prev.TokensOut
		}
		baseline := float64(sum) / float64(i-start)
		tokens := float64(sp.TokensIn + sp.TokensOut)
		if baseline <= 0 || tokens < spikeFloor || tokens < spikeFactor*baseline {
			continue
		}
		detail, _ := json.Marshal(map[string]any{
			"tokens": int64(tokens), "baseline": int64(baseline),
		})
		findings = append(findings, store.Finding{
			RunID: sp.RunID, SpanID: sp.ID, Type: "cost_spike",
			Severity: "warning", Detail: string(detail),
		})
	}
	return findings
}

func sortSpansByEnd(spans []store.Span) {
	sort.Slice(spans, func(i, j int) bool {
		if !spans[i].EndedAt.Equal(spans[j].EndedAt) {
			return spans[i].EndedAt.Before(spans[j].EndedAt)
		}
		return spans[i].ID < spans[j].ID
	})
}
