package export

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/store"
)

// capybara findings are runtime, not source-anchored, so results carry a
// logical location (the run and span) rather than a file and line. That is
// valid SARIF and feeds any tool that ingests the format; GitHub code scanning,
// which maps results onto the source diff, is not the target here.
const infoURI = "https://github.com/tonquoc0407/capybara"

var ruleDesc = map[string]string{
	"improvised":        "model answered past a failed tool without acknowledging it",
	"prompt_injection":  "tool or retrieval output carried a prompt-injection directive into the model",
	"unsupported_claim": "answer stated a figure absent from the retrieved documents",
	"unfaithful":        "answer claim unsupported by the retrieved documents (llm judge)",
	"truncated":         "final answer stopped at the token limit, not at completion",
	"secret_leak":       "span content carried a credential or card number",
	"no_progress":       "model repeated the same answer across turns without converging",
	"orphaned_span":     "span stopped reporting while still open, so the process died inside it",
	"wrong_tool":        "agent called the wrong tool for the request (llm judge)",
	"tool_error":        "tool reported an error inside an otherwise successful span",
	"malformed":         "tool output did not match its learned schema",
	"empty_payload":     "tool returned an empty payload",
	"drift":             "tool output shape changed from its established contract",
	"loop":              "a tool was called repeatedly with identical input",
	"cost_spike":        "token usage spiked above the run baseline",
}

// SARIF renders findings as a SARIF 2.1.0 log, one rule per finding type and a
// stable partial fingerprint per result so a re-upload does not duplicate it.
func SARIF(findings []store.Finding, version string) ([]byte, error) {
	rules := make([]sarifRule, 0)
	seen := make(map[string]bool)
	results := make([]sarifResult, 0, len(findings))
	for _, f := range findings {
		if !seen[f.Type] {
			seen[f.Type] = true
			rules = append(rules, sarifRule{
				ID:               f.Type,
				ShortDescription: sarifText{Text: ruleText(f.Type)},
			})
		}
		results = append(results, sarifResult{
			RuleID:  f.Type,
			Level:   sarifLevel(f.Severity),
			Message: sarifText{Text: analyze.FindingLine(f)},
			Locations: []sarifLocation{{LogicalLocations: []sarifLogical{{
				Name:               f.SpanID,
				FullyQualifiedName: f.RunID + "/" + f.SpanID,
				Kind:               "span",
			}}}},
			PartialFingerprints: map[string]string{"capybara/v1": fingerprint(f)},
		})
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })
	log := sarifLog{
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name: "capybara", InformationURI: infoURI,
				SemanticVersion: version, Rules: rules,
			}},
			Results: results,
		}},
	}
	return json.MarshalIndent(log, "", "  ")
}

func ruleText(typ string) string {
	if d, ok := ruleDesc[typ]; ok {
		return d
	}
	return typ
}

func sarifLevel(severity string) string {
	switch severity {
	case "error", "warning", "note":
		return severity
	default:
		return "warning"
	}
}

func fingerprint(f store.Finding) string {
	sum := sha256.Sum256([]byte(f.RunID + "\x00" + f.SpanID + "\x00" + f.Type))
	return hex.EncodeToString(sum[:8])
}

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name            string      `json:"name"`
	InformationURI  string      `json:"informationUri"`
	SemanticVersion string      `json:"semanticVersion,omitempty"`
	Rules           []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string    `json:"id"`
	ShortDescription sarifText `json:"shortDescription"`
}

type sarifText struct {
	Text string `json:"text"`
}

type sarifResult struct {
	RuleID              string            `json:"ruleId"`
	Level               string            `json:"level"`
	Message             sarifText         `json:"message"`
	Locations           []sarifLocation   `json:"locations"`
	PartialFingerprints map[string]string `json:"partialFingerprints"`
}

type sarifLocation struct {
	LogicalLocations []sarifLogical `json:"logicalLocations"`
}

type sarifLogical struct {
	Name               string `json:"name"`
	FullyQualifiedName string `json:"fullyQualifiedName,omitempty"`
	Kind               string `json:"kind,omitempty"`
}
