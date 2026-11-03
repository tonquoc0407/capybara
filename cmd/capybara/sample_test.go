package main

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/tonquoc0407/capybara/internal/store"
)

func TestSampleCreatesOneLabelledRunWithEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.db")
	var out bytes.Buffer
	for range 2 {
		if err := run(context.Background(), []string{"-db", path, "sample"}, &out); err != nil {
			t.Fatal(err)
		}
	}
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	runs, err := st.ListRuns(context.Background())
	if err != nil || len(runs) != 1 || runs[0].Source != "sample" || runs[0].Environment != "demo" || runs[0].Status != "error" {
		t.Fatalf("sample run: %+v, %v", runs, err)
	}
	content, err := st.Contents(context.Background(), "capybara-sample-tool")
	if err != nil || len(content) != 2 {
		t.Fatalf("sample evidence: %+v, %v", content, err)
	}
}
