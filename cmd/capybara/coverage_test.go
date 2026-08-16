package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func spanStore(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "spans.jsonl")
	lines := `{"run":"r1","span":"root","kind":"agent","name":"a","start":"2026-07-30T10:00:00Z","end":"2026-07-30T10:00:09Z"}
{"run":"r1","span":"llm","parent":"root","kind":"llm","name":"chat","start":"2026-07-30T10:00:01Z","end":"2026-07-30T10:00:02Z","attrs":{"unmapped.field":"x"}}
{"run":"r1","span":"tool","parent":"root","kind":"tool","name":"lookup","start":"2026-07-30T10:00:02Z","end":"2026-07-30T10:00:03Z"}
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

func TestCoverageCmdReportsSpanCounts(t *testing.T) {
	db := spanStore(t)
	var buf bytes.Buffer
	if err := coverageCmd(context.Background(), db, nil, &buf); err != nil {
		t.Fatalf("coverageCmd: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "spans") || !strings.Contains(out, "llm") || !strings.Contains(out, "tool") {
		t.Errorf("output = %q", out)
	}
}

func TestCoverageCmdScopesToOneRun(t *testing.T) {
	db := spanStore(t)
	var buf bytes.Buffer
	if err := coverageCmd(context.Background(), db, []string{"r1"}, &buf); err != nil {
		t.Fatalf("coverageCmd: %v", err)
	}
	if !strings.Contains(buf.String(), "spans") {
		t.Errorf("output = %q", buf.String())
	}
}

func TestCoverageCmdRejectsExtraArgs(t *testing.T) {
	db := spanStore(t)
	if err := coverageCmd(context.Background(), db, []string{"r1", "r2"}, io.Discard); err == nil {
		t.Fatal("expected a usage error")
	}
}

func TestCoverageCmdUnknownRunErrors(t *testing.T) {
	db := spanStore(t)
	if err := coverageCmd(context.Background(), db, []string{"nope"}, io.Discard); err == nil {
		t.Fatal("expected an error resolving an unknown run")
	}
}
