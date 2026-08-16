package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonquoc0407/capybara/internal/store"
)

func replayableStore(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "run.jsonl")
	lines := `{"run":"r1","span":"root","kind":"agent","name":"agent","start":"2026-07-30T10:00:00Z","end":"2026-07-30T10:00:03Z","attrs":{"capybara.entrypoint":"[\"/bin/sh\",\"-c\",\"exit 0\"]","capybara.cwd":"/tmp"}}
{"run":"r1","span":"llm","parent":"root","kind":"llm","name":"chat","start":"2026-07-30T10:00:01Z","end":"2026-07-30T10:00:02Z","contents":[{"role":"user","body":"hi"},{"role":"assistant","body":"hello"}]}
`
	if err := os.WriteFile(file, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(dir, "t.db")
	if err := run(context.Background(), []string{"-db", db, "import", file}, io.Discard); err != nil {
		t.Fatalf("import: %v", err)
	}
	return db
}

// The fixture's entrypoint is a stand-in shell, not a real capybara-sdk
// runner, so the replay process itself fails; what this proves is that
// replayCmd still links a new run to its parent before that process ever
// starts, the same guarantee internal/replay's own Run tests check.
func TestReplayCmdLinksTheReplayToItsParentEvenOnRunnerFailure(t *testing.T) {
	db := replayableStore(t)
	if err := replayCmd(context.Background(), db, false, []string{"r1"}, io.Discard); err == nil {
		t.Fatal("expected the stand-in entrypoint to fail")
	}
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	runs, err := st.ListRuns(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range runs {
		if r.ParentRunID == "r1" {
			return
		}
	}
	t.Fatal("replay run was not linked to its parent")
}

func TestReplayCmdRejectsMissingRun(t *testing.T) {
	db := replayableStore(t)
	if err := replayCmd(context.Background(), db, false, nil, io.Discard); err == nil {
		t.Fatal("expected a usage error")
	}
}

func TestReplayCmdOutputNeedsSpan(t *testing.T) {
	db := replayableStore(t)
	out := filepath.Join(t.TempDir(), "override.txt")
	if err := os.WriteFile(out, []byte(`{"price":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := replayCmd(context.Background(), db, false, []string{"-output", out, "r1"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "-output needs -span") {
		t.Fatalf("err = %v, want -output-needs--span", err)
	}
}

func TestReplayCmdUnknownRunErrors(t *testing.T) {
	db := replayableStore(t)
	if err := replayCmd(context.Background(), db, false, []string{"nope"}, io.Discard); err == nil {
		t.Fatal("expected an error resolving an unknown run")
	}
}
