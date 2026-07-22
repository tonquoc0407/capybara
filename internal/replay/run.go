package replay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

// ingestWait bounds how long Run waits for the replay's spans to be stored.
const ingestWait = 5 * time.Second

// Run links the replay to its parent, hands the manifest to the SDK runner and
// waits for it. The replay's spans arrive over OTLP like any other run.
func Run(ctx context.Context, st *store.Store, m Manifest) error {
	if len(m.Entrypoint) == 0 {
		return fmt.Errorf("manifest has no entrypoint")
	}
	if err := st.SetRunParent(ctx, m.RunID, "otlp", m.ParentRunID); err != nil {
		return err
	}
	path, cleanup, err := writeManifest(m)
	if err != nil {
		return err
	}
	defer cleanup()
	cmd := exec.CommandContext(ctx, m.Entrypoint[0], "-m", "capybara.replay", path)
	cmd.Dir = m.Cwd
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("replay %s: %w: %s", shortID(m.RunID), err, lastLine(output.String()))
	}
	waitForSpans(ctx, st, m.RunID)
	return nil
}

// waitForSpans blocks until the replay's spans have been ingested: the runner
// exits as soon as it has flushed them, a moment before they are stored.
func waitForSpans(ctx context.Context, st *store.Store, runID string) {
	deadline := time.Now().Add(ingestWait)
	for time.Now().Before(deadline) {
		spans, err := st.Spans(ctx, runID)
		if err == nil && len(spans) > 0 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func writeManifest(m Manifest) (string, func(), error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return "", nil, fmt.Errorf("marshal manifest: %w", err)
	}
	path := filepath.Join(os.TempDir(), "capybara-replay-"+m.RunID+".json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return "", nil, fmt.Errorf("write manifest: %w", err)
	}
	return path, func() { _ = os.Remove(path) }, nil
}

// lastLine keeps the runner's final message, which is where it reports why a
// replay stopped.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return "no output"
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
