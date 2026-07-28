package analyze

import (
	"sort"
	"strings"

	"github.com/tonquoc0407/capybara/internal/store"
)

// aiNamespaces are the attribute-key namespaces capybara maps to a gen-AI span
// kind in the otlp ingest. A KindOther span carrying one of these is a variant
// the mapper did not recognise - a real coverage gap - as opposed to a db or
// http span that is other by nature. Kept in step with the convention constants
// in internal/ingest/otlp/semconv.go.
var aiNamespaces = map[string]bool{
	"gen_ai":        true,
	"llm":           true,
	"traceloop":     true,
	"ai":            true,
	"mcp":           true,
	"openinference": true,
}

// Coverage summarises how much of a span set capybara typed, and which
// attribute namespaces on the untyped remainder point at an unmapped convention.
type Coverage struct {
	Total    int
	ByKind   map[store.Kind]int
	Prefixes []PrefixCount // namespaces on KindOther spans, most frequent first
}

// PrefixCount is how many KindOther spans carry an attribute namespace, and
// whether that namespace is one capybara maps elsewhere (an actionable gap).
type PrefixCount struct {
	Prefix string
	Count  int
	AI     bool
}

// SpanCoverage tallies span kinds and, for the untyped remainder, the attribute
// namespaces that reveal an unmapped ingest convention.
func SpanCoverage(spans []store.Span) Coverage {
	cov := Coverage{Total: len(spans), ByKind: make(map[store.Kind]int)}
	perPrefix := make(map[string]int)
	for _, sp := range spans {
		cov.ByKind[sp.Kind]++
		if sp.Kind != store.KindOther {
			continue
		}
		for ns := range namespaces(sp.Attrs.Raw) {
			perPrefix[ns]++
		}
	}
	for ns, n := range perPrefix {
		cov.Prefixes = append(cov.Prefixes, PrefixCount{Prefix: ns, Count: n, AI: aiNamespaces[ns]})
	}
	sort.Slice(cov.Prefixes, func(i, j int) bool {
		if cov.Prefixes[i].Count != cov.Prefixes[j].Count {
			return cov.Prefixes[i].Count > cov.Prefixes[j].Count
		}
		return cov.Prefixes[i].Prefix < cov.Prefixes[j].Prefix
	})
	return cov
}

// namespaces is the set of first key segments in a span's raw attributes, so
// gen_ai.request.model and gen_ai.usage.input_tokens both count as gen_ai once.
func namespaces(raw map[string]any) map[string]bool {
	set := make(map[string]bool)
	for k := range raw {
		ns := k
		if i := strings.IndexByte(k, '.'); i >= 0 {
			ns = k[:i]
		}
		set[ns] = true
	}
	return set
}
