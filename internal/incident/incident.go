// Package incident groups observed failures by a deterministic trace signature.
package incident

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/tonquoc0407/capybara/internal/store"
)

type Group struct {
	Fingerprint string   `json:"fingerprint"`
	Runs        []string `json:"runs"`
	Count       int      `json:"count"`
	FirstError  string   `json:"first_error"`
	Project     string   `json:"project"`
}

// Fingerprint uses tool order, first failed/signal position, finding types and
// schema changes. Duplicate detector signals do not split an incident. It is a
// grouping hint, never proof that two failures share a root cause.
func Fingerprint(spans []store.Span, findings []store.Finding) (string, string) {
	var tools []string
	firstError := ""
	errorType := ""
	errorAt, signalAt := -1, -1
	positions := make(map[string]int, len(spans))
	for i, sp := range spans {
		positions[sp.ID] = i
		if sp.Kind == store.KindTool {
			name := sp.Attrs.ToolName
			if name == "" {
				name = sp.Name
			}
			tools = append(tools, name)
		}
		if firstError == "" && sp.Status == "error" {
			firstError = string(sp.Kind) + ":" + sp.Name
			errorAt = i
			for _, key := range []string{"error.type", "exception.type", "http.response.status_code"} {
				if value := sp.Attrs.Raw[key]; value != nil && value != "" {
					raw, err := json.Marshal(value)
					if err == nil {
						errorType = string(raw)
						break
					}
				}
			}
		}
	}
	typeSet, schemaSet := map[string]bool{}, map[string]bool{}
	for _, f := range findings {
		typeSet[f.Type] = true
		if pos, ok := positions[f.SpanID]; ok && (signalAt < 0 || pos < signalAt) {
			signalAt = pos
		}
		if f.Type == "drift" {
			schemaSet[schemaChange(f.Detail)] = true
		}
	}
	types, schemas := make([]string, 0, len(typeSet)), make([]string, 0, len(schemaSet))
	for typ := range typeSet {
		types = append(types, typ)
	}
	for schema := range schemaSet {
		schemas = append(schemas, schema)
	}
	sort.Strings(types)
	sort.Strings(schemas)
	// JSON encoding keeps arbitrary tool names from colliding with separators.
	sig, _ := json.Marshal(struct {
		Version               int
		Tools                 []string
		FirstError, ErrorType string
		ErrorAt, SignalAt     int
		Types, Schemas        []string
	}{
		2, tools, firstError, errorType, errorAt, signalAt, types, schemas,
	})
	h := sha256.Sum256(sig)
	return hex.EncodeToString(h[:8]), firstError
}

func schemaChange(detail string) string {
	var shape struct {
		Missing []string `json:"missing"`
		Retyped []struct {
			Field string `json:"field"`
			Want  string `json:"want"`
			Got   string `json:"got"`
		} `json:"retyped"`
	}
	if err := json.Unmarshal([]byte(detail), &shape); err != nil {
		return "unreadable schema change"
	}
	if len(shape.Missing) == 0 {
		shape.Missing = nil
	}
	if len(shape.Retyped) == 0 {
		shape.Retyped = nil
	}
	sort.Strings(shape.Missing)
	sort.Slice(shape.Retyped, func(i, j int) bool {
		a, b := shape.Retyped[i], shape.Retyped[j]
		if a.Field != b.Field {
			return a.Field < b.Field
		}
		if a.Want != b.Want {
			return a.Want < b.Want
		}
		return a.Got < b.Got
	})
	raw, _ := json.Marshal(shape)
	return string(raw)
}

// List groups the most recent runs with an observed error or a detector finding.
func List(ctx context.Context, st *store.Store, project string, limit int) ([]Group, error) {
	return ListForSearch(ctx, st, store.RunSearch{Project: project, Limit: limit})
}

func ListForSearch(ctx context.Context, st *store.Store, filter store.RunSearch) ([]Group, error) {
	filter.OnlyIncidents = true
	runs, err := st.SearchRuns(ctx, filter)
	if err != nil {
		return nil, err
	}
	groups := []Group{}
	index := map[string]int{}
	for _, r := range runs {
		if r.Status != "error" && r.Findings == 0 {
			continue
		}
		spans, err := st.Spans(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		findings, err := st.Findings(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		fp, first := Fingerprint(spans, findings)
		key := r.Project + ":" + fp
		if idx, ok := index[key]; ok {
			groups[idx].Runs = append(groups[idx].Runs, r.ID)
			groups[idx].Count++
		} else {
			index[key] = len(groups)
			groups = append(groups, Group{Fingerprint: fp, Runs: []string{r.ID}, Count: 1,
				FirstError: first, Project: r.Project})
		}
	}
	return groups, nil
}
