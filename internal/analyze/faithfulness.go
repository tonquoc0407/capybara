package analyze

import (
	"context"
	"sort"
	"strings"
	"unicode"

	"github.com/tonquoc0407/capybara/internal/store"
)

// Faithfulness detection is the deterministic, offline half of the RAG check: it
// flags a distinctive figure in the final answer that appears in none of the
// documents the run retrieved. A figure is the one claim a structural pass can
// judge without a model - a paraphrased fact needs the opt-in llm judge - and it
// is the most common fabrication a grounded answer commits. Small counts and
// list indices are ignored, so only numbers a source would have supplied count.
func (a *Analyzer) faithfulnessRun(ctx context.Context, rc *runContext) ([]store.Finding, error) {
	var docs []string
	for _, sp := range rc.spans {
		if sp.Kind != store.KindRetrieval {
			continue
		}
		cs, err := a.st.Contents(ctx, sp.ID)
		if err != nil {
			return nil, err
		}
		for _, c := range cs {
			if c.Role == "output" {
				docs = append(docs, c.Body)
			}
		}
	}
	if len(docs) == 0 {
		return nil, nil
	}
	answer, span, ok, err := a.finalAnswer(ctx, rc)
	if err != nil {
		return nil, err
	}
	if !ok || !rc.fresh[span.ID] {
		return nil, nil
	}
	grounded := make(map[string]bool)
	for _, d := range docs {
		for n := range distinctiveNumbers(d) {
			grounded[n] = true
		}
	}
	var unsupported []string
	for n := range distinctiveNumbers(answer) {
		if !grounded[n] {
			unsupported = append(unsupported, n)
		}
	}
	if len(unsupported) == 0 {
		return nil, nil
	}
	sort.Strings(unsupported)
	return []store.Finding{finding(span, "unsupported_claim", "warning", map[string]any{
		"evidence": strings.Join(unsupported, ", "),
	})}, nil
}

// finalAnswer is the run's last llm turn that produced assistant text.
func (a *Analyzer) finalAnswer(ctx context.Context, rc *runContext) (string, store.Span, bool, error) {
	for i := len(rc.llms) - 1; i >= 0; i-- {
		text, err := a.assistantText(ctx, rc.llms[i])
		if err != nil {
			return "", store.Span{}, false, err
		}
		if strings.TrimSpace(text) != "" {
			return text, rc.llms[i], true, nil
		}
	}
	return "", store.Span{}, false, nil
}

// distinctiveNumbers returns the normalized figures in s worth grounding: at
// least three digits, so a price or a precise value counts but a small count or
// a list index does not. Thousands separators are dropped so 4,200 and 4200 are
// the same figure.
func distinctiveNumbers(s string) map[string]bool {
	nums := make(map[string]bool)
	var cur strings.Builder
	flush := func() {
		tok := strings.Trim(cur.String(), ".,")
		cur.Reset()
		if tok == "" {
			return
		}
		digits := 0
		for _, r := range tok {
			if unicode.IsDigit(r) {
				digits++
			}
		}
		if digits >= 3 {
			nums[strings.ReplaceAll(tok, ",", "")] = true
		}
	}
	for _, r := range s {
		if unicode.IsDigit(r) || r == '.' || r == ',' {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return nums
}
