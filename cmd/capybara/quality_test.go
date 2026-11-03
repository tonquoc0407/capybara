package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

func TestQualityCommandsGatePairedRegression(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "quality.db")
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"before-run", "after-run"} {
		sp := store.Span{ID: id + "-span", RunID: id, Kind: store.KindAgent,
			Name: "answer", StartedAt: time.Now(), EndedAt: time.Now(), Status: "ok"}
		if err := st.WriteBatch(ctx, store.Batch{Source: "test", Spans: []store.Span{sp},
			Contents: []store.Content{
				{SpanID: sp.ID, Role: "user", Seq: 0, Body: "question", MediaType: "text/plain"},
				{SpanID: sp.ID, Role: "assistant", Seq: 1, Body: "answer", MediaType: "text/plain"},
			}}); err != nil {
			t.Fatal(err)
		}
	}
	st.Close()
	command := func(args ...string) (string, error) {
		var out bytes.Buffer
		err := run(ctx, append([]string{"-db", db}, args...), &out)
		return strings.TrimSpace(out.String()), err
	}
	dataset, err := command("dataset", "create", "cases")
	if err != nil {
		t.Fatal(err)
	}
	item, err := command("dataset", "add", dataset, "before-run")
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(t.TempDir(), "expected.txt")
	if err := os.WriteFile(expected, []byte("reviewed answer\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := command("dataset", "expect", dataset, item, expected); err != nil || !strings.Contains(output, "reviewed answer") {
		t.Fatalf("expected command: %q, %v", output, err)
	}
	before, err := command("experiment", "create", dataset, "before")
	if err != nil {
		t.Fatal(err)
	}
	after, err := command("experiment", "create", dataset, "after")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := command("dataset", "expect", dataset, item, expected); !errors.Is(err, store.ErrDatasetFrozen) {
		t.Fatalf("expected command edited frozen data: %v", err)
	}
	if clone, err := command("dataset", "clone", dataset, "revised"); err != nil || clone == dataset || clone == "" {
		t.Fatalf("clone command: %q, %v", clone, err)
	}
	if _, err := command("score", "add", "-item", item, "-name", "quality", "-value", "0.8", "before-run"); err != nil {
		t.Fatal(err)
	}
	if _, err := command("score", "add", "-item", item, "-name", "quality", "-value", "0.6", "after-run"); err != nil {
		t.Fatal(err)
	}
	if _, err := command("experiment", "record", before, item, "before-run", "quality"); err != nil {
		t.Fatal(err)
	}
	if _, err := command("experiment", "record", after, item, "after-run", "quality"); err != nil {
		t.Fatal(err)
	}
	output, err := command("experiment", "compare", "-score", "quality", "-min-delta", "0", before, after)
	if !errors.Is(err, errBelowThreshold) || !strings.Contains(output, `"delta":-0.2`) {
		t.Fatalf("compare: %q, %v", output, err)
	}
}
