package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/tonquoc0407/capybara/internal/store"
)

// A baseline is the set of findings a run is already known to carry, so a CI
// gate can fail on what a change newly introduced rather than on the standing
// total. A finding's identity is its run, span and type - the same triple the
// SARIF fingerprint hashes - so detail edits are not counted as regressions.
type baseline struct {
	Version  int             `json:"version"`
	Findings []baselineEntry `json:"findings"`
}

type baselineEntry struct {
	Run  string `json:"run"`
	Span string `json:"span"`
	Type string `json:"type"`
}

func identity(f store.Finding) string {
	return f.RunID + "\x00" + f.SpanID + "\x00" + f.Type
}

// writeBaseline records the current findings as the accepted set, sorted so the
// committed file diffs cleanly.
func writeBaseline(path string, findings []store.Finding) error {
	b := baseline{Version: 1, Findings: make([]baselineEntry, 0, len(findings))}
	for _, f := range findings {
		b.Findings = append(b.Findings, baselineEntry{Run: f.RunID, Span: f.SpanID, Type: f.Type})
	}
	sort.Slice(b.Findings, func(i, j int) bool {
		a, c := b.Findings[i], b.Findings[j]
		if a.Run != c.Run {
			return a.Run < c.Run
		}
		if a.Span != c.Span {
			return a.Span < c.Span
		}
		return a.Type < c.Type
	})
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("encode baseline: %w", err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		return fmt.Errorf("write baseline: %w", err)
	}
	return nil
}

func readBaseline(path string) (map[string]bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read baseline: %w", err)
	}
	var b baseline
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	set := make(map[string]bool, len(b.Findings))
	for _, e := range b.Findings {
		set[e.Run+"\x00"+e.Span+"\x00"+e.Type] = true
	}
	return set, nil
}

// newFindings keeps the findings absent from the baseline: the regressions a
// change introduced.
func newFindings(findings []store.Finding, accepted map[string]bool) []store.Finding {
	out := make([]store.Finding, 0, len(findings))
	for _, f := range findings {
		if !accepted[identity(f)] {
			out = append(out, f)
		}
	}
	return out
}
