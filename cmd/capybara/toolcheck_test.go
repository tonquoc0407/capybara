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

type replyJudge struct{ reply string }

func (r replyJudge) Complete(_ context.Context, _, _ string) (string, error) {
	return r.reply, nil
}

func toolStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "tools.jsonl")
	lines := `{"run":"r1","span":"agent","kind":"agent","name":"a","start":"2026-07-30T10:00:00Z","end":"2026-07-30T10:00:09Z"}
{"run":"r1","span":"llm","parent":"agent","kind":"llm","name":"chat","start":"2026-07-30T10:00:00Z","end":"2026-07-30T10:00:01Z","contents":[{"role":"user","body":"What is the weather in Oslo?"}]}
{"run":"r1","span":"wx","parent":"agent","kind":"tool","tool":"get_weather","name":"get_weather","start":"2026-07-30T10:00:02Z","end":"2026-07-30T10:00:03Z","contents":[{"role":"input","body":"{\"city\":\"Oslo\"}"}]}
{"run":"r1","span":"del","parent":"agent","kind":"tool","tool":"delete_account","name":"delete_account","start":"2026-07-30T10:00:04Z","end":"2026-07-30T10:00:05Z","contents":[{"role":"input","body":"{\"id\":7}"}]}
`
	if err := os.WriteFile(file, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(dir, "t.db")
	if err := run(context.Background(), []string{"-db", db, "import", file}, io.Discard); err != nil {
		t.Fatalf("import: %v", err)
	}
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestJudgeToolsFlagsWrongCall(t *testing.T) {
	st := toolStore(t)
	fs, err := judgeTools(context.Background(), st, []string{"r1"}, replyJudge{`{"wrong": [2]}`})
	if err != nil {
		t.Fatalf("judgeTools: %v", err)
	}
	if len(fs) != 1 || fs[0].Type != "wrong_tool" || fs[0].SpanID != "del" {
		t.Fatalf("findings = %+v", fs)
	}
	if !strings.Contains(fs[0].Detail, "delete_account") {
		t.Errorf("detail = %q", fs[0].Detail)
	}
}

func TestJudgeToolsOutOfRangeIgnored(t *testing.T) {
	st := toolStore(t)
	fs, err := judgeTools(context.Background(), st, []string{"r1"}, replyJudge{`{"wrong": [9]}`})
	if err != nil || len(fs) != 0 {
		t.Fatalf("findings = %+v, err = %v", fs, err)
	}
}

func TestToolcheckUnconfiguredErrors(t *testing.T) {
	t.Setenv("CAPYBARA_JUDGE_URL", "")
	t.Setenv("CAPYBARA_JUDGE_MODEL", "")
	err := toolcheckCmd(context.Background(), filepath.Join(t.TempDir(), "t.db"), nil, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "no judge configured") {
		t.Fatalf("err = %v, want no-judge-configured", err)
	}
}
