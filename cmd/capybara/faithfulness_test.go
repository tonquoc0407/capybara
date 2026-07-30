package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonquoc0407/capybara/internal/store"
)

type fakeJudge struct{ claims []string }

func (f fakeJudge) Complete(_ context.Context, _, _ string) (string, error) {
	b, _ := json.Marshal(map[string]any{"faithful": len(f.claims) == 0, "unsupported": f.claims})
	return string(b), nil
}

func ragStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "rag.jsonl")
	lines := `{"run":"r1","span":"root","kind":"agent","name":"a","start":"2026-07-30T10:00:00Z","end":"2026-07-30T10:00:09Z"}
{"run":"r1","span":"ret","parent":"root","kind":"retrieval","name":"retrieve","start":"2026-07-30T10:00:00Z","end":"2026-07-30T10:00:02Z","contents":[{"role":"output","body":"The plan price is 42 dollars."}]}
{"run":"r1","span":"llm","parent":"root","kind":"llm","name":"chat","start":"2026-07-30T10:00:03Z","end":"2026-07-30T10:00:05Z","contents":[{"role":"assistant","body":"The plan price is 99 dollars."}]}
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

func TestJudgeRunsFlagsUnfaithful(t *testing.T) {
	st := ragStore(t)
	fs, err := judgeRuns(context.Background(), st, []string{"r1"}, fakeJudge{claims: []string{"the price is 99 dollars"}})
	if err != nil {
		t.Fatalf("judgeRuns: %v", err)
	}
	if len(fs) != 1 || fs[0].Type != "unfaithful" || fs[0].Severity != "note" || fs[0].SpanID != "llm" {
		t.Fatalf("findings = %+v", fs)
	}
	if !strings.Contains(fs[0].Detail, "the price is 99 dollars") {
		t.Errorf("detail = %q", fs[0].Detail)
	}
}

func TestJudgeRunsFaithfulWritesNothing(t *testing.T) {
	st := ragStore(t)
	fs, err := judgeRuns(context.Background(), st, []string{"r1"}, fakeJudge{})
	if err != nil || len(fs) != 0 {
		t.Fatalf("findings = %+v, err = %v", fs, err)
	}
}

func TestFaithfulnessUnconfiguredErrors(t *testing.T) {
	t.Setenv("CAPYBARA_JUDGE_URL", "")
	t.Setenv("CAPYBARA_JUDGE_MODEL", "")
	dir := t.TempDir()
	err := faithfulnessCmd(context.Background(), filepath.Join(dir, "t.db"), nil, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "no judge configured") {
		t.Fatalf("err = %v, want no-judge-configured", err)
	}
}
