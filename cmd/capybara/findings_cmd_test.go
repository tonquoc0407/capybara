package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func toolErrorStore(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "run.jsonl")
	lines := `{"run":"r1","span":"root","kind":"agent","name":"a","start":"2026-07-30T10:00:00Z","end":"2026-07-30T10:00:09Z"}
{"run":"r1","span":"tool","parent":"root","kind":"tool","name":"lookup","tool":"lookup","start":"2026-07-30T10:00:01Z","end":"2026-07-30T10:00:02Z","contents":[{"role":"output","body":"{\"error\":true,\"message\":\"boom\"}"}]}
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

func TestFindingsCmdPrintsAndFailsOnMatch(t *testing.T) {
	db := toolErrorStore(t)
	var buf bytes.Buffer
	err := findingsCmd(context.Background(), db, []string{"--fail-on", "tool_error"}, &buf)
	if !errors.Is(err, errFindings) {
		t.Fatalf("err = %v, want errFindings", err)
	}
	if !strings.Contains(buf.String(), "tool_error") {
		t.Errorf("output = %q", buf.String())
	}
}

func TestFindingsCmdNoFailOnPassesEvenWithFindings(t *testing.T) {
	db := toolErrorStore(t)
	if err := findingsCmd(context.Background(), db, nil, io.Discard); err != nil {
		t.Fatalf("findingsCmd: %v", err)
	}
}

func TestFindingsCmdSarif(t *testing.T) {
	db := toolErrorStore(t)
	var buf bytes.Buffer
	if err := findingsCmd(context.Background(), db, []string{"--sarif"}, &buf); err != nil {
		t.Fatalf("findingsCmd: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("output is not valid json: %v\n%s", err, buf.String())
	}
	if doc["version"] != "2.1.0" {
		t.Errorf("version = %v, want 2.1.0", doc["version"])
	}
}

func TestFindingsCmdWriteAndUseBaseline(t *testing.T) {
	db := toolErrorStore(t)
	baseline := filepath.Join(t.TempDir(), "baseline.json")
	if err := findingsCmd(context.Background(), db, []string{"--write-baseline", baseline}, io.Discard); err != nil {
		t.Fatalf("write-baseline: %v", err)
	}
	if _, err := os.Stat(baseline); err != nil {
		t.Fatalf("baseline not written: %v", err)
	}
	var buf bytes.Buffer
	err := findingsCmd(context.Background(), db, []string{"--baseline", baseline, "--fail-on", "any"}, &buf)
	if err != nil {
		t.Fatalf("baselined findings should not breach: %v", err)
	}
	if strings.Contains(buf.String(), "tool_error") {
		t.Errorf("baselined finding should be suppressed, got %q", buf.String())
	}
}

func TestFindingsCmdRejectsExtraArgs(t *testing.T) {
	db := toolErrorStore(t)
	if err := findingsCmd(context.Background(), db, []string{"r1", "r2"}, io.Discard); err == nil {
		t.Fatal("expected a usage error")
	}
}

func TestFindingsCmdScopesToOneRun(t *testing.T) {
	db := toolErrorStore(t)
	var buf bytes.Buffer
	if err := findingsCmd(context.Background(), db, []string{"r1"}, &buf); err != nil {
		t.Fatalf("findingsCmd: %v", err)
	}
	if !strings.Contains(buf.String(), "tool_error") {
		t.Errorf("output = %q", buf.String())
	}
}

func TestFindingsCmdUnknownRunErrors(t *testing.T) {
	db := toolErrorStore(t)
	if err := findingsCmd(context.Background(), db, []string{"nope"}, io.Discard); err == nil {
		t.Fatal("expected an error resolving an unknown run")
	}
}

func TestShort8(t *testing.T) {
	if got := short8("abcdefghij"); got != "abcdefgh" {
		t.Errorf("short8 = %q, want abcdefgh", got)
	}
	if got := short8("short"); got != "short" {
		t.Errorf("short8 = %q, want short unchanged", got)
	}
}
