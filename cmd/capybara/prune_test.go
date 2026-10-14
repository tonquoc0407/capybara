package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

func TestPruneCmd(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "capybara.db")
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}

	for i := 1; i <= 4; i++ {
		now := time.Now().UTC().Add(-time.Duration(10-i) * time.Hour)
		err := st.WriteBatch(ctx, store.Batch{
			Source: "test",
			Spans: []store.Span{
				{
					ID:        "s" + string(rune('0'+i)),
					RunID:     "r" + string(rune('0'+i)),
					Kind:      store.KindAgent,
					Name:      "agent",
					StartedAt: now,
					EndedAt:   now.Add(time.Second),
					Status:    "ok",
				},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	st.Close()

	var out bytes.Buffer
	if err := pruneCmd(ctx, db, []string{"-keep", "2", "-dry-run"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "would prune 2 run(s)") {
		t.Fatalf("unexpected output: %s", out.String())
	}

	out.Reset()
	if err := pruneCmd(ctx, db, []string{"-keep", "2"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "pruned 2 run(s)") {
		t.Fatalf("unexpected output: %s", out.String())
	}

	out.Reset()
	if err := vacuumCmd(ctx, db, nil, &out); err != nil {
		t.Fatal(err)
	}
}

