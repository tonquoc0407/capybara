package tui

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/tonquoc0407/capybara/internal/store"
	"github.com/tonquoc0407/capybara/internal/theme"
)

func seededStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	batch := store.Batch{
		Source: "test",
		Spans: []store.Span{
			span("root", "", store.KindAgent, "agent_loop", 0, 10),
			span("llm1", "root", store.KindLLM, "chat", 1, 2),
		},
		Contents: []store.Content{
			{SpanID: "llm1", Role: "user", Seq: 0, Body: "needle body text", MediaType: "text/plain"},
		},
	}
	if err := st.WriteBatch(context.Background(), batch); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	return st
}

func startApp(t *testing.T, st *store.Store) *teatest.TestModel {
	t.Helper()
	ch, cancel := st.Subscribe()
	t.Cleanup(cancel)
	return teatest.NewTestModel(t, newApp(st, theme.Bara(), ch, true),
		teatest.WithInitialTermSize(110, 32))
}

// waitFor asserts all wants appear in one accumulated read: successive WaitFor
// calls drain the output, so a frame can only be matched once.
func waitFor(t *testing.T, tm *teatest.TestModel, wants ...string) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		for _, w := range wants {
			if !bytes.Contains(bts, []byte(w)) {
				return false
			}
		}
		return true
	}, teatest.WithDuration(5*time.Second))
}

func quitApp(t *testing.T, tm *teatest.TestModel) {
	t.Helper()
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

func TestAppRendersThreePanes(t *testing.T) {
	tm := startApp(t, seededStore(t))
	waitFor(t, tm, "agent_loop", "span tree", "detail", "run r1")
	quitApp(t, tm)
}

func TestAppShowsContentOnSelection(t *testing.T) {
	tm := startApp(t, seededStore(t))
	waitFor(t, tm, "agent_loop")
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}) // select llm1
	waitFor(t, tm, "needle body text")
	quitApp(t, tm)
}

func TestAppStreamsLiveSpans(t *testing.T) {
	st := seededStore(t)
	tm := startApp(t, st)
	waitFor(t, tm, "agent_loop")
	late := store.Batch{Source: "test", Spans: []store.Span{
		span("late", "root", store.KindTool, "late_streamed_tool", 6, 1),
	}}
	if err := st.WriteBatch(context.Background(), late); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	waitFor(t, tm, "late_streamed_tool")
	quitApp(t, tm)
}

func TestAppShowsFindings(t *testing.T) {
	st := seededStore(t)
	b := store.Batch{Source: "test", Findings: []store.Finding{
		{RunID: "r1", Type: "parse_error", Severity: "warning", Detail: `{"line":7}`},
	}}
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	tm := startApp(t, st)
	waitFor(t, tm, "1 finding", "! r1")
	quitApp(t, tm)
}

func TestAppDiffFlow(t *testing.T) {
	st := seededStore(t) // run r1
	other := store.Batch{
		Source: "test",
		Spans: []store.Span{
			{
				ID: "root2", RunID: "r2", Kind: store.KindAgent, Name: "agent_loop",
				StartedAt: base.Add(time.Hour), EndedAt: base.Add(time.Hour + 10*time.Second),
				Status: "ok",
			},
			{
				ID: "llm2", RunID: "r2", ParentID: "root2", Kind: store.KindLLM, Name: "chat",
				StartedAt: base.Add(time.Hour + time.Second),
				EndedAt:   base.Add(time.Hour + 3*time.Second), Status: "ok",
			},
		},
		Contents: []store.Content{
			{SpanID: "llm2", Role: "user", Seq: 0, Body: "a different question", MediaType: "text/plain"},
		},
	}
	if err := st.WriteBatch(context.Background(), other); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	tm := startApp(t, st)
	waitFor(t, tm, "agent_loop")
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")}) // mark selected run
	waitFor(t, tm, "select second run")
	tm.Send(tea.KeyMsg{Type: tea.KeyShiftTab})                  // focus runs pane
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}) // other run
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")}) // diff
	waitFor(t, tm, "diff r", " vs ", "first divergence")
	quitApp(t, tm)
}

// The re-run flow starts with e: the recorded output goes through $EDITOR and
// the replacement is held until r.
func TestAppEditStagesReplacementOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in editor is a shell script")
	}
	st := seededStore(t)
	tool := store.Batch{
		Source: "test",
		Spans:  []store.Span{span("tool1", "root", store.KindTool, "lookup_price", 3, 1)},
		Contents: []store.Content{
			{SpanID: "tool1", Role: "output", Seq: 0, Body: `{"price":42}`, MediaType: "application/json"},
		},
	}
	if err := st.WriteBatch(context.Background(), tool); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	editor := filepath.Join(t.TempDir(), "ed.sh")
	script := "#!/bin/sh\nprintf '{\"price\":99}' > \"$1\"\n"
	if err := os.WriteFile(editor, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// editorCommand() checks $VISUAL before $EDITOR; clear it so a host
	// shell's own VISUAL can't shadow the stand-in editor here.
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", editor)
	tm := startApp(t, st)
	waitFor(t, tm, "lookup_price")
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}) // select the tool span
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	waitFor(t, tm, "edited lookup_price", "press r to re-run")
	quitApp(t, tm)
}

func TestAppEditRejectsSpansWithoutToolOutput(t *testing.T) {
	tm := startApp(t, seededStore(t))
	waitFor(t, tm, "agent_loop")
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")}) // agent span
	waitFor(t, tm, "e edits a tool span")
	quitApp(t, tm)
}

func TestStatusViewShowsHintUntilFirstTabOrEnter(t *testing.T) {
	m := newApp(seededStore(t), theme.Bara(), nil, true)
	m.width, m.height = 110, 32
	m.layout()
	if !strings.Contains(m.statusView(), "tab: switch panes") {
		t.Error("hint missing before any tab/enter")
	}
	model, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	m = model.(appModel)
	if strings.Contains(m.statusView(), "tab: switch panes") {
		t.Error("hint still shown after the first tab press")
	}
}

func TestPaneMarksOnlyTheFocusedOne(t *testing.T) {
	m := newApp(seededStore(t), theme.Bara(), nil, true)
	focused := m.pane("runs", "body", 30, 10, true)
	unfocused := m.pane("runs", "body", 30, 10, false)
	if !strings.Contains(focused, "> runs") {
		t.Errorf("focused pane has no > marker:\n%s", focused)
	}
	if strings.Contains(unfocused, "> runs") {
		t.Errorf("unfocused pane carries the > marker:\n%s", unfocused)
	}
}

func TestAppHelpOverlay(t *testing.T) {
	tm := startApp(t, seededStore(t))
	waitFor(t, tm, "agent_loop")
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	waitFor(t, tm, "tab")
	quitApp(t, tm)
}
