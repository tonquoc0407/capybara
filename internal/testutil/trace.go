// Package testutil provides deterministic trace fixtures for performance tests.
package testutil

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

// TraceBatch makes n spans spread over runs of at most 100 spans: one agent
// root followed by alternating model and tool calls, with unique tool inputs.
// Small recorded bodies keep storage/analysis costs separate from payload size.
func TraceBatch(n int) store.Batch {
	at := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	batch := store.Batch{
		Source:   "benchmark",
		Spans:    make([]store.Span, 0, n),
		Contents: make([]store.Content, 0, 2*n),
	}
	for i := range n {
		runID := fmt.Sprintf("run-%06d", i/100)
		id := fmt.Sprintf("span-%06d", i)
		rootID := fmt.Sprintf("span-%06d", i/100*100)
		start := at.Add(time.Duration(i) * time.Second)
		sp := store.Span{
			ID: id, RunID: runID, ParentID: rootID,
			Kind: store.KindLLM, Name: "chat", Status: "ok",
			StartedAt: start, EndedAt: start.Add(time.Millisecond),
			TokensIn: 100, TokensOut: 20,
			Attrs: store.Attrs{Model: "gpt-4o"},
		}
		switch {
		case i%100 == 0:
			sp.ParentID, sp.Kind, sp.Name = "", store.KindAgent, "agent"
			sp.TokensIn, sp.TokensOut, sp.Attrs = 0, 0, store.Attrs{}
			sp.EndedAt = start.Add(100 * time.Second)
		case i%2 == 0:
			sp.Kind, sp.Name, sp.Attrs = store.KindTool, "lookup", store.Attrs{ToolName: "lookup"}
			sp.TokensIn, sp.TokensOut = 0, 0
			batch.Contents = append(batch.Contents,
				store.Content{SpanID: id, Role: "input", Seq: 0, Body: fmt.Sprintf(`{"item":%d}`, i), MediaType: "application/json"},
				store.Content{SpanID: id, Role: "output", Seq: 1, Body: `{"value":42}`, MediaType: "application/json"},
			)
		default:
			batch.Contents = append(batch.Contents,
				store.Content{SpanID: id, Role: "user", Seq: 0, Body: "Look up the next item.", MediaType: "text/plain"},
				store.Content{SpanID: id, Role: "assistant", Seq: 1, Body: fmt.Sprintf("Item %d returned a value.", i), MediaType: "text/plain"},
			)
		}
		batch.Spans = append(batch.Spans, sp)
	}
	return batch
}

// LargeToolBatch makes one small run whose tool output is outputSize bytes.
// It keeps large-payload storage and analysis measurements separate from span
// count measurements.
func LargeToolBatch(outputSize int) store.Batch {
	at := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	body := strings.Repeat("x", outputSize)
	return store.Batch{
		Source: "benchmark",
		Spans: []store.Span{
			{ID: "large-root", RunID: "large-run", Kind: store.KindAgent, Name: "agent",
				StartedAt: at, EndedAt: at.Add(time.Second), Status: "ok"},
			{ID: "large-tool", RunID: "large-run", ParentID: "large-root", Kind: store.KindTool,
				Name: "lookup", StartedAt: at, EndedAt: at.Add(time.Second), Status: "ok",
				Attrs: store.Attrs{ToolName: "lookup"}},
		},
		Contents: []store.Content{
			{SpanID: "large-tool", Role: "output", Seq: 0, Body: body, MediaType: "text/plain"},
		},
	}
}

// JSONL makes the same deterministic workload as TraceBatch in the generic
// span-per-line import format. A non-zero toolOutputSize exercises large
// recorded tool bodies without changing the span topology.
func JSONL(n, toolOutputSize int) string {
	var out strings.Builder
	for i := range n {
		at := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Second)
		line := map[string]any{
			"run":    fmt.Sprintf("run-%06d", i/100),
			"span":   fmt.Sprintf("span-%06d", i),
			"name":   "chat",
			"start":  at,
			"end":    at.Add(time.Millisecond),
			"status": "ok",
		}
		switch {
		case i%100 == 0:
			line["kind"], line["name"] = "agent", "agent"
		case i%2 == 0:
			line["kind"], line["name"] = "tool", "lookup"
			line["tool"] = "lookup"
			body := `{"value":42}`
			if toolOutputSize > 0 {
				body = strings.Repeat("x", toolOutputSize)
			}
			line["contents"] = []map[string]string{{"role": "output", "body": body}}
		default:
			line["kind"], line["model"] = "llm", "gpt-4o"
		}
		data, err := json.Marshal(line)
		if err != nil {
			panic(err)
		}
		out.Write(data)
		out.WriteByte('\n')
	}
	return out.String()
}
