package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonquoc0407/capybara/internal/store"
)

func replayableStore(t *testing.T) (db, marker string) {
	t.Helper()
	dir := t.TempDir()
	python := ""
	for _, name := range []string{"python3", "python"} {
		path, err := exec.LookPath(name)
		if err == nil {
			python = path
			break
		}
	}
	if python == "" {
		t.Skip("Python is required to exercise the Python replay runner")
	}
	pkg := filepath.Join(dir, "capybara")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "__init__.py"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	marker = filepath.Join(dir, "runner marker")
	source := "import pathlib, sys\npathlib.Path('runner marker').write_text('ran')\nprint('fake replay ran', file=sys.stderr)\nraise SystemExit(23)\n"
	if err := os.WriteFile(filepath.Join(pkg, "replay.py"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	entrypoint, err := json.Marshal([]string{python, "agent.py"})
	if err != nil {
		t.Fatal(err)
	}
	attrs, err := json.Marshal(map[string]string{
		"capybara.entrypoint": string(entrypoint),
		"capybara.cwd":        dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "run.jsonl")
	lines := `{"run":"r1","span":"root","kind":"agent","name":"agent","start":"2026-07-30T10:00:00Z","end":"2026-07-30T10:00:03Z","attrs":` + string(attrs) + `}
{"run":"r1","span":"llm","parent":"root","kind":"llm","name":"chat","start":"2026-07-30T10:00:01Z","end":"2026-07-30T10:00:02Z","contents":[{"role":"user","body":"hi"},{"role":"assistant","body":"hello"}]}
`
	if err := os.WriteFile(file, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	db = filepath.Join(dir, "t.db")
	if err := run(context.Background(), []string{"-db", db, "import", file}, io.Discard); err != nil {
		t.Fatalf("import: %v", err)
	}
	return db, marker
}

func TestReplayCmdLinksTheReplayToItsParentEvenOnRunnerFailure(t *testing.T) {
	db, marker := replayableStore(t)
	err := replayCmd(context.Background(), db, false, []string{"r1"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "fake replay ran") {
		t.Fatalf("replayCmd = %v, want the fake runner's failure", err)
	}
	if raw, err := os.ReadFile(marker); err != nil || string(raw) != "ran" {
		t.Fatalf("runner marker = %q, %v", raw, err)
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
	db, _ := replayableStore(t)
	if err := replayCmd(context.Background(), db, false, nil, io.Discard); err == nil {
		t.Fatal("expected a usage error")
	}
}

func TestReplayCmdOutputNeedsSpan(t *testing.T) {
	db, _ := replayableStore(t)
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
	db, _ := replayableStore(t)
	if err := replayCmd(context.Background(), db, false, []string{"nope"}, io.Discard); err == nil {
		t.Fatal("expected an error resolving an unknown run")
	}
}
