package export

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/tonquoc0407/capybara/internal/analyze"
)

// Divergence is one way a run departed from a golden snapshot.
type Divergence struct {
	Tool   string
	Span   string
	Reason string
	Golden string
	Run    string
	Detail string
}

// Check compares a run against a golden snapshot, matching calls by tool name
// and input so a reordered run still lines up. Nil when the run reproduces it.
func Check(golden, run Fixture) []Divergence {
	var out []Divergence
	if golden.Status != "" && golden.Status != run.Status {
		out = append(out, Divergence{
			Span: "run", Reason: "status changed",
			Golden: golden.Status, Run: run.Status,
		})
	}
	if len(golden.Spans) > 0 {
		used := make([]bool, len(run.Spans))
		for _, want := range golden.Spans {
			match := -1
			for i, got := range run.Spans {
				if !used[i] && want.Kind == got.Kind && want.Name == got.Name &&
					want.Status == got.Status {
					match = i
					break
				}
			}
			if match >= 0 {
				used[match] = true
				continue
			}
			for i, got := range run.Spans {
				if !used[i] && want.Kind == got.Kind && want.Name == got.Name {
					match = i
					break
				}
			}
			if match >= 0 {
				used[match] = true
				got := run.Spans[match]
				out = append(out, Divergence{Span: want.Name, Reason: "status changed",
					Golden: want.Status, Run: got.Status})
			} else {
				out = append(out, Divergence{Span: want.Name, Reason: "span not recorded",
					Golden: string(want.Kind) + " " + want.Status})
			}
		}
		for i, extra := range run.Spans {
			if !used[i] {
				out = append(out, Divergence{Span: extra.Name, Reason: "span not in golden",
					Run: string(extra.Kind) + " " + extra.Status})
			}
		}
	}
	byHash := make(map[string]ToolFixture, len(run.Tools))
	for _, tf := range run.Tools {
		byHash[tf.Hash] = tf
	}
	matched := make(map[string]bool, len(golden.Tools))
	for _, want := range golden.Tools {
		matched[want.Hash] = true
		got, ok := byHash[want.Hash]
		if !ok {
			out = append(out, Divergence{Tool: want.Tool, Reason: "not called", Golden: want.Output})
			continue
		}
		if got.Output == want.Output {
			continue
		}
		out = append(out, Divergence{
			Tool:   want.Tool,
			Reason: "output changed",
			Golden: want.Output,
			Run:    got.Output,
			Detail: analyze.SchemaViolation(want.Schema, got.Output),
		})
	}
	for _, tf := range run.Tools {
		if !matched[tf.Hash] {
			out = append(out, Divergence{Tool: tf.Tool, Reason: "not in golden", Run: tf.Output})
		}
	}
	return out
}

// ReadFixture loads a snapshot written by WriteGolden.
func ReadFixture(path string) (Fixture, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Fixture{}, fmt.Errorf("read golden: %w", err)
	}
	var fx Fixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		return Fixture{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return fx, nil
}
