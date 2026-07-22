package claude

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/tonquoc0407/capybara/internal/store"
)

// Files untouched for longer than this at startup are old sessions: they are
// skipped until they change again, then read in full.
const staleAfter = 24 * time.Hour

// DefaultRoot returns ~/.claude/projects, or "" when it does not exist.
func DefaultRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	root := filepath.Join(home, ".claude", "projects")
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return ""
	}
	return root
}

// Watch tails Claude Code session logs under root until ctx is cancelled.
func Watch(ctx context.Context, st *store.Store, root string, captureContent bool) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("claude watch: %w", err)
	}
	defer w.Close()
	tailers := make(map[string]*tailer)
	if err := watchTree(ctx, st, w, tailers, root, captureContent); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			if err := handleEvent(ctx, st, w, tailers, ev, captureContent); err != nil {
				return err
			}
		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			return fmt.Errorf("claude watch: %w", err)
		}
	}
}

// watchTree registers root and its project dirs, tailing recent session files.
func watchTree(ctx context.Context, st *store.Store, w *fsnotify.Watcher,
	tailers map[string]*tailer, root string, capture bool,
) error {
	if err := w.Add(root); err != nil {
		return fmt.Errorf("claude watch %s: %w", root, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("claude watch %s: %w", root, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if err := w.Add(dir); err != nil {
			return fmt.Errorf("claude watch %s: %w", dir, err)
		}
		files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
		if err != nil {
			return fmt.Errorf("claude watch %s: %w", dir, err)
		}
		for _, path := range files {
			info, err := os.Stat(path)
			if err != nil || time.Since(info.ModTime()) > staleAfter {
				continue // read in full if it ever changes again
			}
			t := newTailer(path, capture)
			tailers[path] = t
			if err := t.consume(ctx, st); err != nil {
				return err
			}
		}
	}
	return nil
}

func handleEvent(ctx context.Context, st *store.Store, w *fsnotify.Watcher,
	tailers map[string]*tailer, ev fsnotify.Event, capture bool,
) error {
	if !ev.Op.Has(fsnotify.Create) && !ev.Op.Has(fsnotify.Write) {
		return nil
	}
	if ev.Op.Has(fsnotify.Create) {
		if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
			if err := w.Add(ev.Name); err != nil {
				return fmt.Errorf("claude watch %s: %w", ev.Name, err)
			}
			return nil
		}
	}
	if !strings.HasSuffix(ev.Name, ".jsonl") {
		return nil
	}
	t, ok := tailers[ev.Name]
	if !ok {
		t = newTailer(ev.Name, capture)
		tailers[ev.Name] = t
	}
	return t.consume(ctx, st)
}

// tailer reads one session file incrementally, holding a partial trailing line.
type tailer struct {
	path    string
	capture bool
	offset  int64
	partial []byte
	m       *mapper
}

func newTailer(path string, capture bool) *tailer {
	return &tailer{path: path, capture: capture, m: newMapper(runIDForFile(path), capture)}
}

func (t *tailer) consume(ctx context.Context, st *store.Store) error {
	f, err := os.Open(t.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil // raced with deletion; nothing to read
		}
		return fmt.Errorf("claude tail %s: %w", t.path, err)
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && info.Size() < t.offset {
		// Truncated or replaced: remap the whole file from scratch.
		t.offset, t.partial = 0, nil
		t.m = newMapper(t.m.runID, t.capture)
	}
	if _, err := f.Seek(t.offset, io.SeekStart); err != nil {
		return fmt.Errorf("claude tail %s: %w", t.path, err)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return fmt.Errorf("claude tail %s: %w", t.path, err)
	}
	t.offset += int64(len(data))
	data = append(t.partial, data...)
	batch := store.Batch{Source: "claude"}
	label := ""
	for {
		nl := bytes.IndexByte(data, '\n')
		if nl < 0 {
			break
		}
		lineBytes := bytes.TrimSpace(data[:nl])
		data = data[nl+1:]
		if len(lineBytes) == 0 {
			continue
		}
		delta, l := t.m.process(lineBytes)
		mergeBatch(&batch, delta)
		if l != "" {
			label = l
		}
	}
	t.partial = data
	if err := st.WriteBatch(ctx, batch); err != nil {
		return fmt.Errorf("claude tail %s: %w", t.path, err)
	}
	if label != "" {
		if err := st.SetRunLabel(ctx, t.m.runID, "claude", label); err != nil {
			return fmt.Errorf("claude tail %s: %w", t.path, err)
		}
	}
	return nil
}

func mergeBatch(dst *store.Batch, src store.Batch) {
	for _, sp := range src.Spans {
		dst.Spans = upsertSpan(dst.Spans, sp)
	}
	dst.Contents = append(dst.Contents, src.Contents...)
	dst.Findings = append(dst.Findings, src.Findings...)
}
