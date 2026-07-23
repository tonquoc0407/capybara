package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonquoc0407/capybara/internal/store"
)

func TestDiffCommand(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "test.db")
	lineA := `{"run":"run-aaa","span":"a1","kind":"tool","tool":"search_db","name":"search_db","start":"2026-07-22T10:00:00Z","end":"2026-07-22T10:00:01Z","contents":[{"role":"output","body":"{\"price\":42}"}]}` + "\n"
	lineB := `{"run":"run-bbb","span":"b1","kind":"tool","tool":"search_db","name":"search_db","start":"2026-07-22T11:00:00Z","end":"2026-07-22T11:00:02Z","contents":[{"role":"output","body":"{\"price\":\"42\"}"}]}` + "\n"
	file := filepath.Join(dir, "runs.jsonl")
	if err := os.WriteFile(file, []byte(lineA+lineB), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"-db", db, "import", file}, io.Discard); err != nil {
		t.Fatalf("import: %v", err)
	}
	var out strings.Builder
	if err := run(context.Background(), []string{"-db", db, "diff", "run-a", "run-b"}, &out); err != nil {
		t.Fatalf("diff: %v", err)
	}
	got := out.String()
	for _, want := range []string{"diff run-aaa run-bbb", "* tool search_db", "first divergence", `{"price":42}`, `{"price":"42"}`} {
		if !strings.Contains(got, want) {
			t.Errorf("diff output missing %q:\n%s", want, got)
		}
	}
}

func TestDiffCommandRejectsAmbiguousPrefix(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "test.db")
	line := `{"run":"run-aaa","span":"a1","kind":"llm","name":"chat"}` + "\n" +
		`{"run":"run-abb","span":"b1","kind":"llm","name":"chat"}` + "\n"
	file := filepath.Join(dir, "runs.jsonl")
	if err := os.WriteFile(file, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"-db", db, "import", file}, io.Discard); err != nil {
		t.Fatalf("import: %v", err)
	}
	err := run(context.Background(), []string{"-db", db, "diff", "run-a", "run-ab"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("err = %v, want ambiguous", err)
	}
}

func TestServeRejectsExtraArguments(t *testing.T) {
	err := run(context.Background(), []string{"serve", "some-run"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "usage: capybara serve") {
		t.Errorf("run(serve some-run) = %v, want usage error", err)
	}
}

func TestServeStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	db := filepath.Join(t.TempDir(), "test.db")
	var out strings.Builder
	if err := run(ctx, []string{"-db", db, "serve", "-addr", "127.0.0.1:0"}, &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	if !strings.HasPrefix(out.String(), "http://127.0.0.1:") {
		t.Errorf("serve printed %q, want the bound address", out.String())
	}
}

// improviseJSONL is an agent turn whose answer leans on a failed tool: root
// agent, one llm turn, a failing tool, then an llm answer that cites it.
const improviseJSONL = `{"run":"blame-run","span":"root","parent":"","kind":"agent","name":"agent_loop","start":"2026-07-22T10:00:00Z","end":"2026-07-22T10:00:10Z","status":"ok"}
{"run":"blame-run","span":"llm1","parent":"root","kind":"llm","name":"chat","start":"2026-07-22T10:00:00Z","end":"2026-07-22T10:00:02Z","status":"ok"}
{"run":"blame-run","span":"tool1","parent":"llm1","kind":"tool","tool":"search_db","name":"search_db","start":"2026-07-22T10:00:02Z","end":"2026-07-22T10:00:03Z","status":"error","contents":[{"role":"output","body":"connection refused"}]}
{"run":"blame-run","span":"llm2","parent":"root","kind":"llm","name":"chat","start":"2026-07-22T10:00:04Z","end":"2026-07-22T10:00:06Z","status":"ok","contents":[{"role":"assistant","body":"The search_db results show the price is 42."}]}
`

func TestBlameCommand(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "test.db")
	file := filepath.Join(dir, "runs.jsonl")
	if err := os.WriteFile(file, []byte(improviseJSONL), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"-db", db, "import", file}, io.Discard); err != nil {
		t.Fatalf("import: %v", err)
	}
	var out strings.Builder
	if err := run(context.Background(), []string{"-db", db, "blame", "blame-run"}, &out); err != nil {
		t.Fatalf("blame: %v", err)
	}
	got := out.String()
	for _, want := range []string{"blame blame-ru", "agent_loop", "improvised after search_db failure", "root", "search_db"} {
		if !strings.Contains(got, want) {
			t.Errorf("blame output missing %q:\n%s", want, got)
		}
	}
}

func TestBlameCleanRunPrintsNothing(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "test.db")
	file := filepath.Join(dir, "runs.jsonl")
	line := `{"run":"clean","span":"root","kind":"agent","name":"loop","start":"2026-07-22T10:00:00Z","end":"2026-07-22T10:00:01Z","status":"ok"}` + "\n"
	if err := os.WriteFile(file, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"-db", db, "import", file}, io.Discard); err != nil {
		t.Fatalf("import: %v", err)
	}
	var out strings.Builder
	if err := run(context.Background(), []string{"-db", db, "blame", "clean"}, &out); err != nil {
		t.Fatalf("blame: %v", err)
	}
	if out.String() != "" {
		t.Errorf("blame of clean run printed %q, want nothing", out.String())
	}
}

func TestExportGoldenCommand(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "test.db")
	file := filepath.Join(dir, "runs.jsonl")
	line := `{"run":"gold","span":"t1","kind":"tool","tool":"fetch","name":"fetch","start":"2026-07-22T10:00:00Z","end":"2026-07-22T10:00:01Z","status":"ok","contents":[{"role":"input","body":"{\"sku\":\"A\"}"},{"role":"output","body":"{\"price\":42}"}]}` + "\n"
	if err := os.WriteFile(file, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"-db", db, "import", file}, io.Discard); err != nil {
		t.Fatalf("import: %v", err)
	}
	out := filepath.Join(dir, "tests")
	var stdout strings.Builder
	if err := run(context.Background(), []string{"-db", db, "export", "--golden", "-o", out, "gold"}, &stdout); err != nil {
		t.Fatalf("export: %v", err)
	}
	path := strings.TrimSpace(stdout.String())
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %q: %v", path, err)
	}
	for _, want := range []string{`"tool": "fetch"`, `"price"`, `"hash"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("golden missing %q:\n%s", want, raw)
		}
	}
}

func TestExportRequiresOneRun(t *testing.T) {
	err := run(context.Background(), []string{"export"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "usage: capybara export") {
		t.Errorf("run(export) = %v, want usage error", err)
	}
}

func TestExportWithoutGoldenWritesAPytestCase(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "test.db")
	importLine(t, db, dir, "run.jsonl", toolRunJSONL("pytest", `{"price":42}`))
	var stdout strings.Builder
	args := []string{"-db", db, "export", "-o", filepath.Join(dir, "tests"), "pytest"}
	if err := run(context.Background(), args, &stdout); err != nil {
		t.Fatalf("export: %v", err)
	}
	paths := strings.Fields(stdout.String())
	if len(paths) != 2 {
		t.Fatalf("printed %v, want a fixture and a test", paths)
	}
	src, err := os.ReadFile(paths[1])
	if err != nil {
		t.Fatalf("read test: %v", err)
	}
	for _, want := range []string{"def test_tool_contracts", "def test_live_tool_contracts"} {
		if !strings.Contains(string(src), want) {
			t.Errorf("generated test missing %q", want)
		}
	}
}

// toolRunJSONL is one completed tool call, the unit capybara check compares.
func toolRunJSONL(runID, output string) string {
	return `{"run":"` + runID + `","span":"` + runID + `-t","kind":"tool","tool":"fetch",` +
		`"name":"fetch","start":"2026-07-22T10:00:00Z","end":"2026-07-22T10:00:01Z","status":"ok",` +
		`"contents":[{"role":"input","body":"{\"sku\":\"A\"}"},{"role":"output","body":"` +
		strings.ReplaceAll(output, `"`, `\"`) + `"}]}` + "\n"
}

func importLine(t *testing.T, db, dir, name, line string) {
	t.Helper()
	file := filepath.Join(dir, name)
	if err := os.WriteFile(file, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"-db", db, "import", file}, io.Discard); err != nil {
		t.Fatalf("import %s: %v", name, err)
	}
}

func goldenOf(t *testing.T, db, dir, runID string) string {
	t.Helper()
	var stdout strings.Builder
	args := []string{"-db", db, "export", "--golden", "-o", filepath.Join(dir, "tests"), runID}
	if err := run(context.Background(), args, &stdout); err != nil {
		t.Fatalf("export golden: %v", err)
	}
	return strings.TrimSpace(stdout.String())
}

func TestCheckReportsDivergenceFromGolden(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "test.db")
	importLine(t, db, dir, "gold.jsonl", toolRunJSONL("gold", `{"price":42,"currency":"USD"}`))
	golden := goldenOf(t, db, dir, "gold")
	importLine(t, db, dir, "later.jsonl", toolRunJSONL("later", `{"price":42}`))
	var out strings.Builder
	err := run(context.Background(), []string{"-db", db, "check", golden, "later"}, &out)
	if !errors.Is(err, errDiverged) {
		t.Fatalf("check = %v, want errDiverged", err)
	}
	got := out.String()
	for _, want := range []string{"fetch", "output changed", "currency", "1 divergence"} {
		if !strings.Contains(got, want) {
			t.Errorf("check output missing %q:\n%s", want, got)
		}
	}
}

func TestCheckSilentWhenRunMatchesGolden(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "test.db")
	importLine(t, db, dir, "gold.jsonl", toolRunJSONL("gold", `{"price":42,"currency":"USD"}`))
	golden := goldenOf(t, db, dir, "gold")
	importLine(t, db, dir, "same.jsonl", toolRunJSONL("same", `{"price":42,"currency":"USD"}`))
	var out strings.Builder
	if err := run(context.Background(), []string{"-db", db, "check", golden, "same"}, &out); err != nil {
		t.Fatalf("check: %v", err)
	}
	if out.String() != "" {
		t.Errorf("matching run printed %q, want nothing", out.String())
	}
}

func TestWatchRequiresClaudeSource(t *testing.T) {
	for _, args := range [][]string{{"watch"}, {"watch", "cursor"}} {
		err := run(context.Background(), args, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "usage: capybara watch claude") {
			t.Errorf("run(%v) = %v, want usage error", args, err)
		}
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	err := run(context.Background(), []string{"bogus"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("run(bogus) = %v, want unknown-command error", err)
	}
}

func TestHelpPrintsUsage(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"-h"}} {
		var b strings.Builder
		if err := run(context.Background(), args, &b); err != nil {
			t.Fatalf("run(%v) = %v", args, err)
		}
		if !strings.HasPrefix(b.String(), "usage: capybara") {
			t.Errorf("run(%v) output does not start with usage header: %q", args, b.String())
		}
	}
}

func TestImportRequiresOneFile(t *testing.T) {
	err := run(context.Background(), []string{"import"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "usage: capybara import") {
		t.Errorf("run(import) = %v, want usage error", err)
	}
}

func TestImportRejectsUnknownExtension(t *testing.T) {
	err := run(context.Background(), []string{"import", "trace.txt"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), ".jsonl") {
		t.Errorf("run(import trace.txt) = %v, want format error", err)
	}
}

func TestImportAgentReplayJSON(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "trace.json")
	trace := `{"agent_name":"bot","status":"completed","session_id":"s1",` +
		`"started_at":"2026-07-22T10:00:00Z","ended_at":"2026-07-22T10:00:05Z",` +
		`"steps":[{"step_number":1,"step_type":"llm_call","name":"plan"}]}`
	if err := os.WriteFile(file, []byte(trace), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	db := filepath.Join(dir, "test.db")
	if err := run(context.Background(), []string{"-db", db, "import", file}, io.Discard); err != nil {
		t.Fatalf("run(import) = %v", err)
	}
	st, err := store.Open(db)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()
	runs, err := st.ListRuns(context.Background())
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != "s1" || runs[0].Label != "bot" {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestImportWritesToStore(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "trace.jsonl")
	line := `{"run":"r1","span":"root","kind":"agent","name":"loop","start":"2026-07-22T10:00:00Z","end":"2026-07-22T10:00:01Z"}` + "\n"
	if err := os.WriteFile(file, []byte(line), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	db := filepath.Join(dir, "test.db")
	if err := run(context.Background(), []string{"-db", db, "import", file}, io.Discard); err != nil {
		t.Fatalf("run(import) = %v", err)
	}
	st, err := store.Open(db)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()
	runs, err := st.ListRuns(context.Background())
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != "r1" {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestReceiverModeStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	db := filepath.Join(t.TempDir(), "test.db")
	if err := run(ctx, []string{"-db", db}, io.Discard); err != nil {
		t.Fatalf("run() = %v, want nil on cancelled context", err)
	}
}
