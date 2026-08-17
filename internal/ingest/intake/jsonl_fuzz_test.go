package intake

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/tonquoc0407/capybara/internal/store"
)

// ImportJSONL parses whatever file a user hands to `capybara import`, which
// may come from a hand-rolled export script or another tool entirely - it
// must reject malformed input with an error, never panic on it.
func FuzzImportJSONL(f *testing.F) {
	f.Add([]byte(`{"run":"r1","span":"s1","kind":"llm","name":"chat"}`))
	f.Add([]byte(`{"run":"r1","span":"s1","kind":"tool","tool":"lookup","attrs":{"a":1},"contents":[{"role":"input","body":"{}"}]}`))
	f.Add([]byte(``))
	f.Add([]byte(`not json`))
	f.Add([]byte(`{"run":"r1"}`))
	f.Add([]byte(`{"run":"r1","span":"s1","start":"not-a-time"}`))
	f.Add([]byte(`{"run":"r1","span":"s1","contents":[{"role":"input"}]}`))
	st, err := store.Open(filepath.Join(f.TempDir(), "test.db"))
	if err != nil {
		f.Fatalf("Open: %v", err)
	}
	f.Cleanup(func() { st.Close() })
	f.Fuzz(func(_ *testing.T, data []byte) {
		_ = ImportJSONL(context.Background(), st, bytes.NewReader(data), true)
	})
}
