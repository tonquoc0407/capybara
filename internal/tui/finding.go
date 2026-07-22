package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tonquoc0407/capybara/internal/store"
)

type findingDetail struct {
	Tool    string   `json:"tool"`
	Missing []string `json:"missing"`
	Retyped []struct {
		Field string `json:"field"`
		Want  string `json:"want"`
		Got   string `json:"got"`
	} `json:"retyped"`
	Want     string   `json:"want"`
	Line     int      `json:"line"`
	Error    string   `json:"error"`
	Cause    string   `json:"cause"`
	Evidence string   `json:"evidence"`
	Pattern  []string `json:"pattern"`
	Tokens   int64    `json:"tokens"`
	Baseline int64    `json:"baseline"`
}

func parseDetail(f store.Finding) findingDetail {
	var d findingDetail
	_ = json.Unmarshal([]byte(f.Detail), &d) // a bad detail still renders by type
	return d
}

// findingSummary is the one-line form shown under the span in the tree.
func findingSummary(f store.Finding) string {
	d := parseDetail(f)
	switch f.Type {
	case "drift":
		extra := len(d.Missing) + len(d.Retyped) - 1
		switch {
		case len(d.Missing) > 0 && extra > 0:
			return fmt.Sprintf("missing field: %s (+%d)", d.Missing[0], extra)
		case len(d.Missing) > 0:
			return "missing field: " + d.Missing[0]
		case len(d.Retyped) > 0 && extra > 0:
			return fmt.Sprintf("retyped field: %s (+%d)", d.Retyped[0].Field, extra)
		case len(d.Retyped) > 0:
			return "retyped field: " + d.Retyped[0].Field
		}
		return "drift"
	case "malformed":
		if d.Want != "" {
			return "malformed output, want " + d.Want
		}
		return "malformed output"
	case "empty_payload":
		return "empty payload"
	case "improvised":
		return "improvised after " + d.Tool + " failure"
	case "parse_error":
		return fmt.Sprintf("parse error at line %d", d.Line)
	case "loop":
		return "tool loop: " + strings.Join(d.Pattern, ", ")
	case "cost_spike":
		return fmt.Sprintf("token spike: %d vs %d baseline", d.Tokens, d.Baseline)
	}
	return f.Type
}

// findingLines is the expanded form for the detail pane: summary plus the
// schema diff or evidence.
func findingLines(f store.Finding) []string {
	d := parseDetail(f)
	lines := []string{f.Type + ": " + findingSummary(f)}
	for _, field := range d.Missing {
		lines = append(lines, "  missing: "+field)
	}
	for _, r := range d.Retyped {
		lines = append(lines, fmt.Sprintf("  %s: want %s, got %s", r.Field, r.Want, r.Got))
	}
	if f.Type == "improvised" {
		lines = append(lines, "  cause: "+d.Cause, "  evidence: "+d.Evidence)
	}
	if f.Type == "parse_error" && d.Error != "" {
		lines = append(lines, "  "+d.Error)
	}
	return lines
}
