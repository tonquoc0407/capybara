package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/replay"
	"github.com/tonquoc0407/capybara/internal/store"
)

type (
	// editReadyMsg carries a recorded output written out for $EDITOR.
	editReadyMsg struct {
		span store.Span
		path string
	}
	editDoneMsg struct {
		span store.Span
		path string
		err  error
	}
	replayDoneMsg struct {
		parentRunID string
		runID       string
		err         error
	}
)

// edit is the value the next re-run substitutes for a span's recorded output.
type edit struct {
	runID  string
	spanID string
	name   string
	output string
}

// handleEditKey opens the selected tool span's recorded output in $EDITOR.
func (m appModel) handleEditKey() (tea.Model, tea.Cmd) {
	sp, ok := m.middleSelected()
	if !ok || sp.Kind != store.KindTool {
		m.lastErr = errors.New("e edits a tool span's output")
		return m, nil
	}
	st := m.st
	return m, func() tea.Msg {
		contents, err := st.Contents(context.Background(), sp.ID)
		if err != nil {
			return errMsg{err}
		}
		body := ""
		found := false
		for _, c := range contents {
			if c.Role == "output" {
				body, found = c.Body, true
			}
		}
		if !found {
			return errMsg{fmt.Errorf("span %s recorded no output to edit", shortID(sp.ID))}
		}
		path := filepath.Join(os.TempDir(), "capybara-edit-"+sp.ID+".json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			return errMsg{fmt.Errorf("stage edit: %w", err)}
		}
		return editReadyMsg{span: sp, path: path}
	}
}

// handleReplayKey re-runs the selected run, substituting a pending edit.
func (m appModel) handleReplayKey() (tea.Model, tea.Cmd) {
	if m.selectedRun == "" {
		return m, nil
	}
	st, parent, capture := m.st, m.selectedRun, m.capture
	spanID, override := "", ""
	if m.edit.runID == parent {
		spanID, override = m.edit.spanID, m.edit.output
	}
	m.replaying = true
	return m, func() tea.Msg {
		ctx := context.Background()
		manifest, err := replay.Build(ctx, st, parent, spanID, override)
		if err != nil {
			return replayDoneMsg{parentRunID: parent, err: err}
		}
		if err := replay.Run(ctx, st, manifest, capture); err != nil {
			return replayDoneMsg{parentRunID: parent, runID: manifest.RunID, err: err}
		}
		return replayDoneMsg{parentRunID: parent, runID: manifest.RunID}
	}
}

func (m appModel) editorCmd(msg editReadyMsg) tea.Cmd {
	name, args := editorCommand()
	c := exec.Command(name, append(args, msg.path)...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editDoneMsg{span: msg.span, path: msg.path, err: err}
	})
}

func (m appModel) applyEdit(msg editDoneMsg) (tea.Model, tea.Cmd) {
	defer os.Remove(msg.path)
	if msg.err != nil {
		m.lastErr = fmt.Errorf("editor: %w", msg.err)
		return m, nil
	}
	body, err := os.ReadFile(msg.path)
	if err != nil {
		m.lastErr = fmt.Errorf("read edit: %w", err)
		return m, nil
	}
	m.edit = edit{
		runID:  m.selectedRun,
		spanID: msg.span.ID,
		name:   spanLabel(msg.span),
		output: string(body),
	}
	return m, nil
}

// finishReplay opens the diff against the run that was replayed, which is the
// point of the re-run.
func (m appModel) finishReplay(msg replayDoneMsg) (tea.Model, tea.Cmd) {
	m.replaying = false
	if msg.err != nil {
		m.lastErr = msg.err
		return m, nil
	}
	m.edit = edit{}
	st, a, b := m.st, msg.parentRunID, msg.runID
	return m, tea.Batch(m.loadRuns(), func() tea.Msg {
		d, err := analyze.DiffRuns(context.Background(), st, a, b)
		if err != nil {
			return errMsg{err}
		}
		return diffMsg{diff: d}
	})
}

func editorCommand() (string, []string) {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	fields := strings.Fields(editor)
	return fields[0], fields[1:]
}

func spanLabel(sp store.Span) string {
	if sp.Attrs.ToolName != "" {
		return sp.Attrs.ToolName
	}
	return sp.Name
}
