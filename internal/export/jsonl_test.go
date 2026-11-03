package export

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/ingest/intake"
	"github.com/tonquoc0407/capybara/internal/store"
)

func TestSpanJSONLRoundTripPreservesSpansAndContent(t *testing.T) {
	ctx := context.Background()
	source, err := store.Open(filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	at := time.Now().UTC()
	err = source.WriteBatch(ctx, store.Batch{
		Source: "test", Spans: []store.Span{{
			ID: "s", RunID: "r", Kind: store.KindLLM,
			Name: "chat", StartedAt: at, EndedAt: at.Add(time.Second), Status: "ok", TokensIn: 3,
			Attrs: store.Attrs{Model: "model-x", Raw: map[string]any{"key": "value"}},
		}},
		Contents: []store.Content{{SpanID: "s", Role: "user", Body: "hello"}, {SpanID: "s", Role: "assistant", Seq: 1, Body: "world"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := WriteSpanJSONL(ctx, source, "r", &encoded); err != nil {
		t.Fatal(err)
	}
	target, err := store.Open(filepath.Join(t.TempDir(), "target.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	if err := intake.ImportJSONL(ctx, target, bytes.NewReader(encoded.Bytes()), true); err != nil {
		t.Fatal(err)
	}
	spans, err := target.Spans(ctx, "r")
	if err != nil || len(spans) != 1 || spans[0].Attrs.Model != "model-x" || spans[0].TokensIn != 3 || spans[0].Attrs.Raw["key"] != "value" {
		t.Fatalf("spans after roundtrip: %+v, %v", spans, err)
	}
	contents, err := target.Contents(ctx, "s")
	if err != nil || len(contents) != 2 || contents[0].Body != "hello" || contents[1].Body != "world" {
		t.Fatalf("contents after roundtrip: %+v, %v", contents, err)
	}
}
