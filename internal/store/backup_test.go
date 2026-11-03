package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupRestorePreservesSnapshotAndPreviousDB(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "active.db")
	backup := filepath.Join(dir, "snapshot.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.WriteBatch(ctx, Batch{Source: "test", Spans: []Span{{ID: "s1", RunID: "r1", Kind: KindAgent, Name: "first", Status: "ok"}}}); err != nil {
		t.Fatal(err)
	}
	if err := st.Backup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	if err := ValidateBackup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	if err := st.Backup(ctx, backup); err == nil {
		t.Fatal("overwrote existing backup")
	}
	if err := st.WriteBatch(ctx, Batch{Source: "test", Spans: []Span{{ID: "s2", RunID: "r2", Kind: KindAgent, Name: "second", Status: "ok"}}}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	previous, err := RestoreBackup(ctx, dbPath, backup)
	if err != nil {
		t.Fatal(err)
	}
	if previous == "" {
		t.Fatal("did not preserve previous database")
	}
	restored, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	runs, err := restored.ListRuns(ctx)
	if err != nil || len(runs) != 1 || runs[0].ID != "r1" {
		t.Fatalf("restored runs: %+v %v", runs, err)
	}
	restored.Close()
	old, err := Open(previous)
	if err != nil {
		t.Fatal(err)
	}
	oldRuns, err := old.ListRuns(ctx)
	if err != nil || len(oldRuns) != 2 {
		t.Fatalf("previous runs: %+v %v", oldRuns, err)
	}
	old.Close()
}

func TestRestoreRejectsCorruptBackup(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	backup := filepath.Join(dir, "bad.db")
	if err := os.WriteFile(backup, []byte("not a sqlite database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreBackup(ctx, filepath.Join(dir, "active.db"), backup); err == nil {
		t.Fatal("accepted corrupt backup")
	}
}
