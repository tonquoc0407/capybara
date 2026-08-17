package claude

import (
	"strings"
	"testing"
)

// process reads one line of Claude Code's own, unversioned session log - a
// local file capybara tails, and one a malicious or corrupted session could
// still hand it a broken line for. It must error, never panic.
func FuzzProcess(f *testing.F) {
	for _, ln := range strings.Split(strings.TrimSpace(session), "\n") {
		f.Add([]byte(ln))
	}
	f.Add([]byte(``))
	f.Add([]byte(`not json`))
	f.Add([]byte(`{"type":"assistant"}`))
	f.Add([]byte(`{"type":"user","message":{"content":[{"type":"tool_result"}]}}`))
	m := newMapper("fuzz-run", true)
	f.Fuzz(func(_ *testing.T, data []byte) {
		m.process(data)
	})
}
