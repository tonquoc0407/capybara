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
	Reason string
	Golden string
	Run    string
	Detail string
}

// Check compares a run against a golden snapshot, matching calls by tool name
// and input so a reordered run still lines up. Nil when the run reproduces it.
func Check(golden, run Fixture) []Divergence {
	byHash := make(map[string]ToolFixture, len(run.Tools))
	for _, tf := range run.Tools {
		byHash[tf.Hash] = tf
	}
	matched := make(map[string]bool, len(golden.Tools))
	var out []Divergence
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
