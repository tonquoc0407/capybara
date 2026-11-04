package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorBundleContainsCountsWithoutTraceData(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "private-traces.db")
	if err := sampleCmd(context.Background(), db, true, nil, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "diagnostics.json")
	if err := doctorCmd(context.Background(), db, []string{"-bundle", path}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-traces.db", "price service unavailable", "capybara-sample-root"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("bundle contains trace data %q", secret)
		}
	}
	if report["version"] == nil || report["counts"] == nil {
		t.Fatalf("missing diagnostic fields: %s", data)
	}
}
