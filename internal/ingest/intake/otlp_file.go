package intake

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/tonquoc0407/capybara/internal/ingest/otlp"
	"github.com/tonquoc0407/capybara/internal/store"
)

// ImportOTLPJSONL imports the OTLP file exporter's TracesData JSONL format.
// The complete file is validated before anything is committed.
func ImportOTLPJSONL(ctx context.Context, st *store.Store, r io.Reader, captureContent bool) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	batch := store.Batch{Source: "otlp-file", DropContent: !captureContent}
	unmarshal := ptrace.JSONUnmarshaler{}
	for line := 1; sc.Scan(); line++ {
		raw := bytes.TrimSpace(sc.Bytes())
		if len(raw) == 0 {
			continue
		}
		var top map[string]json.RawMessage
		if err := json.Unmarshal(raw, &top); err != nil {
			return fmt.Errorf("line %d: %w", line, err)
		}
		if _, ok := top["resourceSpans"]; !ok {
			return fmt.Errorf("line %d: expected OTLP TracesData with resourceSpans", line)
		}
		traces, err := unmarshal.UnmarshalTraces(raw)
		if err != nil {
			return fmt.Errorf("line %d: decode OTLP traces: %w", line, err)
		}
		mapped := otlp.ToBatch(traces, captureContent)
		batch.Spans = append(batch.Spans, mapped.Spans...)
		batch.Contents = append(batch.Contents, mapped.Contents...)
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read OTLP JSONL: %w", err)
	}
	if err := st.WriteBatch(ctx, batch); err != nil {
		return fmt.Errorf("write OTLP JSONL: %w", err)
	}
	return nil
}
