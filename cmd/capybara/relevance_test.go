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

type fakeRelevanceJudge struct{ reason string }

func (f fakeRelevanceJudge) Complete(_ context.Context, _, _ string) (string, error) {
	b, _ := json.Marshal(map[string]any{"relevant": f.reason == "", "reason": f.reason})
	return string(b), nil
}

func chatStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "chat.jsonl")
	lines := `{"run":"r1","span":"root","kind":"agent","name":"a","start":"2026-07-30T10:00:00Z","end":"2026-07-30T10:00:09Z"}
{"run":"r1","span":"llm","parent":"root","kind":"llm","name":"chat","start":"2026-07-30T10:00:03Z","end":"2026-07-30T10:00:05Z","contents":[{"role":"user","body":"what is the capital of France?"},{"role":"assistant","body":"The weather is sunny today."}]}
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

func TestJudgeRelevanceFlagsOffTopic(t *testing.T) {
	st := chatStore(t)
	fs, err := judgeRelevance(context.Background(), st, []string{"r1"}, fakeRelevanceJudge{reason: "answers a different question"})
	if err != nil {
		t.Fatalf("judgeRelevance: %v", err)
	}
	if len(fs) != 1 || fs[0].Type != "off_topic" || fs[0].Severity != "note" || fs[0].SpanID != "llm" {
		t.Fatalf("findings = %+v", fs)
	}
	if !strings.Contains(fs[0].Detail, "answers a different question") {
		t.Errorf("detail = %q", fs[0].Detail)
	}
}

func TestJudgeRelevanceOnTopicWritesNothing(t *testing.T) {
	st := chatStore(t)
	fs, err := judgeRelevance(context.Background(), st, []string{"r1"}, fakeRelevanceJudge{})
	if err != nil || len(fs) != 0 {
		t.Fatalf("findings = %+v, err = %v", fs, err)
	}
}

func TestRelevanceUnconfiguredErrors(t *testing.T) {
	t.Setenv("CAPYBARA_JUDGE_URL", "")
	t.Setenv("CAPYBARA_JUDGE_MODEL", "")
	dir := t.TempDir()
	err := relevanceCmd(context.Background(), filepath.Join(dir, "t.db"), nil, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "no judge configured") {
		t.Fatalf("err = %v, want no-judge-configured", err)
	}
}
