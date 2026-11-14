package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tonquoc0407/capybara/internal/store"
	"github.com/tonquoc0407/capybara/internal/theme"
)

func TestConnectChecksReceiverAndOpensTestTrace(t *testing.T) {
	events := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/events" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var event map[string]any
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Errorf("decode event: %v", err)
		}
		events <- event
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	m := newApp(nil, theme.Pupu(), nil, true)
	m.width, m.height = 100, 32
	m.about.OTLP = server.URL
	m.connectOpen = true
	m.connectSeen = map[string]bool{}
	updated, cmd := m.handleConnectKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	m = updated.(appModel)
	if cmd == nil || !m.connectChecking {
		t.Fatal("connection check did not start")
	}
	msg, ok := cmd().(connectCheckMsg)
	if !ok || msg.err != nil || msg.runID == "" {
		t.Fatalf("connection check result: %#v", msg)
	}
	event := <-events
	if event["run_id"] != msg.runID || event["kind"] != "agent" || event["name"] != "Capybara connection check" {
		t.Fatalf("unexpected event: %#v", event)
	}
	updated, _ = m.Update(msg)
	m = updated.(appModel)
	if m.connectChecking || m.connectNewRun != msg.runID {
		t.Fatalf("connection check was not recorded: %#v", m.connectNewRun)
	}
	updated, _ = m.Update(runsMsg{runs: []store.Run{{ID: msg.runID}}})
	m = updated.(appModel)
	updated, _ = m.handleConnectKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	m = updated.(appModel)
	if m.connectOpen || m.selectedRun != msg.runID {
		t.Fatalf("test trace did not open: %q", m.selectedRun)
	}
}

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
	if !strings.Contains(view, "HTTP event runs stored: 1") || !strings.Contains(view, "metadata only") || !strings.Contains(view, "Send spans from a local app without an SDK.") {
		t.Fatalf("connect view: %s", view)
	}
	m.notice = "copy requested; paste to confirm"
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(appModel)
	if m.connectIndex != 1 || m.notice != "" {
		t.Fatalf("selection=%d notice=%q", m.connectIndex, m.notice)
	}
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(appModel).connectOpen {
		t.Fatal("Esc did not close Connect")
	}
}

func TestConnectOpensNewlyReceivedTrace(t *testing.T) {
	m := newApp(nil, theme.Bara(), nil, true)
	m.width, m.height = 100, 32
	m.connectOpen = true
	m.connectSeen = map[string]bool{"old": true}
	updated, _ := m.Update(runsMsg{runs: []store.Run{{ID: "old"}, {ID: "fresh", Label: "new trace"}}})
	m = updated.(appModel)
	if m.connectNewRun != "fresh" || !strings.Contains(plain(m.connectView()), "Trace received: fresh") {
		t.Fatalf("new trace not confirmed: %q", m.connectNewRun)
	}
	updated, _ = m.handleConnectKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	m = updated.(appModel)
	if m.connectOpen || m.selectedRun != "fresh" || m.runs.selectedID() != "fresh" {
		t.Fatalf("open trace failed: selected=%q", m.selectedRun)
	}
}

func TestConnectSetupIsReadableOnNarrowTerminal(t *testing.T) {
	m := newApp(nil, theme.Pupu(), nil, true)
	m.width, m.height = 60, 18
	m.about.OTLP = "http://127.0.0.1:4318"
	m.connectOpen = true
	m.connectIndex = 3
	m.connectSetupOpen = true
	first := plain(m.connectView())
	if !strings.Contains(first, "Node SDK") || !strings.Contains(first, "npm ci") {
		t.Fatalf("setup not visible: %s", first)
	}
	m.connectScroll = 8
	later := plain(m.connectView())
	if !strings.Contains(later, "OTEL_EXPORTER_OTLP_ENDPOINT") {
		t.Fatalf("setup did not scroll to final command: %s", later)
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
