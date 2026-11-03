package claude

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/tonquoc0407/capybara/internal/store"
)

// ImportJSONL backfills one Claude Code session, including files older than
// the watcher's startup window. A missing final newline is accepted.
func ImportJSONL(ctx context.Context, st *store.Store, r io.Reader, path string, capture bool) error {
	m := newMapper(runIDForFile(path), capture)
	batch := store.Batch{Source: "claude", DropContent: !capture}
	label := ""
	reader := bufio.NewReader(r)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			line = bytes.TrimSpace(line)
			if len(line) > 0 {
				delta, nextLabel := m.process(line)
				mergeBatch(&batch, delta)
				if nextLabel != "" {
					label = nextLabel
				}
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read Claude session: %w", err)
		}
	}
	if err := st.WriteBatch(ctx, batch); err != nil {
		return fmt.Errorf("write Claude session: %w", err)
	}
	if capture && label != "" {
		return st.SetRunLabel(ctx, m.runID, "claude", label)
	}
	if !capture && len(batch.Spans) > 0 {
		return st.SetRunLabel(ctx, m.runID, "claude", "")
	}
	return nil
}
