package export

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

func TestPromptfooExportUsesRecordedInputAndExpectedOutput(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "cases.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().UTC()
	err = st.WriteBatch(ctx, store.Batch{
		Source:   "test",
		Spans:    []store.Span{{ID: "s", RunID: "r", Kind: store.KindAgent, Name: "agent", StartedAt: now, EndedAt: now.Add(time.Second), Status: "ok"}},
		Contents: []store.Content{{SpanID: "s", Role: "user", Body: "What is 2+2?"}, {SpanID: "s", Role: "assistant", Seq: 1, Body: "4"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	d, err := st.CreateDataset(ctx, "default", "math")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddDatasetItem(ctx, d.ID, "r"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cases.json")
	if err := WritePromptfooCases(ctx, st, d.ID, path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Vars   map[string]string              `json:"vars"`
		Assert []struct{ Type, Value string } `json:"assert"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 1 || cases[0].Vars["input"] != "What is 2+2?" || len(cases[0].Assert) != 1 || cases[0].Assert[0].Type != "equals" || cases[0].Assert[0].Value != "4" {
		t.Fatalf("unexpected cases: %+v", cases)
	}
	if err := WritePromptfooCases(ctx, st, d.ID, path); err == nil {
		t.Fatal("existing export was overwritten")
	}
}
