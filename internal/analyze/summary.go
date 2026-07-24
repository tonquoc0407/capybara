package analyze

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tonquoc0407/capybara/internal/store"
)

// FindingDetail is the decoded detail_json of a finding, the union of the
// fields the detectors record. Empty fields mean the type does not use them.
type FindingDetail struct {
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

// ParseDetail decodes a finding's detail; a malformed one still renders by type.
func ParseDetail(f store.Finding) FindingDetail {
	var d FindingDetail
	_ = json.Unmarshal([]byte(f.Detail), &d)
	return d
}

// FindingSummary is the one-line form of a finding, shared by the tree, the
// blame view and the CLI so a new finding type is worded in one place.
func FindingSummary(f store.Finding) string {
	d := ParseDetail(f)
	switch f.Type {
	case "drift":
		return driftSummary(d)
	case "malformed":
		if d.Want != "" {
			return "malformed output, want " + d.Want
		}
		return "malformed output"
	case "empty_payload":
		return "empty payload"
	case "tool_error":
		return "tool reported an error in its output"
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

// FindingLine labels a summary with its type, except when the summary already
// opens with it — "improvised: improvised after x" helps nobody.
func FindingLine(f store.Finding) string {
	summary := FindingSummary(f)
	if strings.HasPrefix(summary, f.Type) {
		return summary
	}
	return f.Type + ": " + summary
}

func driftSummary(d FindingDetail) string {
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
}
