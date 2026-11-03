package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tonquoc0407/capybara/internal/theme"
)

func TestConnectUsesBoundAddressAndShowsLiveRun(t *testing.T) {
	m := newApp(nil, theme.Bara(), nil, false)
	m.width, m.height = 100, 32
	m.about.OTLP = "http://127.0.0.1:53418"
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	m = updated.(appModel)
	if !m.connectOpen || !strings.Contains(m.connectSnippet(), "53418/api/v1/events") {
		t.Fatalf("connect did not use live listener")
	}
	m.connectRuns = 1
	view := plain(m.View())
	if !strings.Contains(view, "HTTP event runs stored: 1") || !strings.Contains(view, "metadata only") {
		t.Fatalf("connect view: %s", view)
	}
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(appModel).connectOpen {
		t.Fatal("Esc did not close Connect")
	}
}

func TestConnectNodeSnippetUsesSourceBuildUntilPublished(t *testing.T) {
	m := newApp(nil, theme.Bara(), nil, false)
	m.about.OTLP = "http://127.0.0.1:4318"
	m.connectIndex = 3
	snippet := m.connectSnippet()
	if strings.Contains(snippet, "npm install capybara-sdk") ||
		!strings.Contains(snippet, "npm install /path/to/capybara/sdk-js") ||
		!strings.Contains(snippet, "http://127.0.0.1:4318") {
		t.Fatalf("misleading Node connection snippet: %s", snippet)
	}
}

func TestConnectEventSnippetContainsValidJSON(t *testing.T) {
	m := newApp(nil, theme.Bara(), nil, false)
	m.about.OTLP = "http://127.0.0.1:4318"
	snippet := m.connectSnippet()
	start := strings.Index(snippet, "-d '")
	if start < 0 || !strings.HasSuffix(snippet, "'") {
		t.Fatalf("missing JSON body: %s", snippet)
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(snippet[start+4:len(snippet)-1]), &event); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if event["type"] != "span.complete" || event["event_id"] != event["run_id"] || event["run_id"] != event["span_id"] {
		t.Fatalf("inconsistent event ids: %+v", event)
	}
}
