package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvalCmdScoresALabelledCorpus(t *testing.T) {
	db := toolErrorStore(t)
	labels := filepath.Join(t.TempDir(), "labels.json")
	body := `{"types":["tool_error"],"cases":[{"run":"r1","expect":["tool_error"]}]}`
	if err := os.WriteFile(labels, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := evalCmd(context.Background(), db, []string{labels}, &buf); err != nil {
		t.Fatalf("evalCmd: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "tool_error") || !strings.Contains(out, "1.00") {
		t.Errorf("output = %q", out)
	}
}

func TestEvalCmdFailsUnderThreshold(t *testing.T) {
	db := toolErrorStore(t)
	labels := filepath.Join(t.TempDir(), "labels.json")
	// r1 is labelled for a type the fixture never triggers, so recall is 0.
	body := `{"types":["improvised"],"cases":[{"run":"r1","expect":["improvised"]}]}`
	if err := os.WriteFile(labels, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	err := evalCmd(context.Background(), db, []string{"--fail-under", "0.5", labels}, io.Discard)
	if !errors.Is(err, errBelowThreshold) {
		t.Fatalf("err = %v, want errBelowThreshold", err)
	}
}

func TestEvalCmdRejectsMissingArg(t *testing.T) {
	db := toolErrorStore(t)
	if err := evalCmd(context.Background(), db, nil, io.Discard); err == nil {
		t.Fatal("expected a usage error")
	}
}

func TestEvalCmdUnknownRunErrors(t *testing.T) {
	db := toolErrorStore(t)
	labels := filepath.Join(t.TempDir(), "labels.json")
	body := `{"types":["tool_error"],"cases":[{"run":"nope","expect":["tool_error"]}]}`
	if err := os.WriteFile(labels, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := evalCmd(context.Background(), db, []string{labels}, io.Discard); err == nil {
		t.Fatal("expected an error resolving an unlabelled run")
	}
}

func TestRatio(t *testing.T) {
	if got := ratio(0.5, true); got != "0.50" {
		t.Errorf("ratio = %q, want 0.50", got)
	}
	if got := ratio(0, false); got != "-" {
		t.Errorf("ratio = %q, want -", got)
	}
}
