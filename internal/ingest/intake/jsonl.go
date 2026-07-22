// Package intake imports trace files; for now generic span-per-line jsonl.
package intake

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

// spanLine is one line of generic jsonl: one span, its optional contents inline.
type spanLine struct {
	Run       string         `json:"run"`
	Span      string         `json:"span"`
	Parent    string         `json:"parent"`
	Kind      string         `json:"kind"`
	Name      string         `json:"name"`
	Start     time.Time      `json:"start"`
	End       time.Time      `json:"end"`
	TokensIn  int64          `json:"tokens_in"`
	TokensOut int64          `json:"tokens_out"`
	Status    string         `json:"status"`
	Model     string         `json:"model"`
	Provider  string         `json:"provider"`
	Tool      string         `json:"tool"`
	Attrs     map[string]any `json:"attrs"`
	Contents  []struct {
		Role string `json:"role"`
		Body string `json:"body"`
	} `json:"contents"`
}

// ImportJSONL reads span-per-line jsonl from r and writes it as one atomic batch.
func ImportJSONL(ctx context.Context, st *store.Store, r io.Reader, captureContent bool) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	batch := store.Batch{Source: "import"}
	for n := 1; sc.Scan(); n++ {
		raw := bytes.TrimSpace(sc.Bytes())
		if len(raw) == 0 {
			continue
		}
		var ln spanLine
		if err := json.Unmarshal(raw, &ln); err != nil {
			return fmt.Errorf("line %d: %w", n, err)
		}
		if ln.Run == "" || ln.Span == "" {
			return fmt.Errorf("line %d: missing run or span id", n)
		}
		if ln.Status == "" {
			ln.Status = "ok"
		}
		batch.Spans = append(batch.Spans, store.Span{
			ID:        ln.Span,
			RunID:     ln.Run,
			ParentID:  ln.Parent,
			Kind:      store.ParseKind(ln.Kind),
			Name:      ln.Name,
			StartedAt: ln.Start,
			EndedAt:   ln.End,
			TokensIn:  ln.TokensIn,
			TokensOut: ln.TokensOut,
			Status:    ln.Status,
			Attrs: store.Attrs{
				Model:    ln.Model,
				Provider: ln.Provider,
				ToolName: ln.Tool,
				Raw:      ln.Attrs,
			},
		})
		if !captureContent {
			continue
		}
		for i, c := range ln.Contents {
			batch.Contents = append(batch.Contents, store.Content{
				SpanID:    ln.Span,
				Role:      c.Role,
				Seq:       i,
				Body:      c.Body,
				MediaType: mediaType(c.Body),
			})
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read: %w", err)
	}
	if err := st.WriteBatch(ctx, batch); err != nil {
		return fmt.Errorf("write batch: %w", err)
	}
	return nil
}

func mediaType(body string) string {
	if json.Valid([]byte(body)) {
		return "application/json"
	}
	return "text/plain"
}
