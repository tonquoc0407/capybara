package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var connectOptions = []string{"HTTP JSON events", "OTLP", "Python SDK", "Node SDK (source build)", "Claude watcher", "File import"}
var connectDescriptions = []string{
	"Send spans from a local app without an SDK.",
	"Point an instrumented app at the local OTLP receiver.",
	"Add tracing to Python code and export it locally.",
	"Build the Node SDK from this repository, then instrument your app.",
	"Read Claude Code sessions already stored on this machine.",
	"Load a saved trace file into this database.",
}

func (m appModel) handleConnectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "i":
		m.connectOpen = false
	case "up", "k":
		m.connectIndex = (m.connectIndex + len(connectOptions) - 1) % len(connectOptions)
		m.notice = ""
	case "down", "j":
		m.connectIndex = (m.connectIndex + 1) % len(connectOptions)
		m.notice = ""
	case "y", "enter":
		snippet := m.connectSnippet()
		if snippet != "" {
			_ = copyToClipboard(snippet)
			m.notice = "copy requested; paste to confirm"
		}
	}
	return m, nil
}

func (m appModel) connectSnippet() string {
	base := m.about.OTLP
	switch m.connectIndex {
	case 0:
		if base == "" {
			return ""
		}
		at := m.connectDemoAt
		if at.IsZero() {
			at = time.Now().UTC()
		}
		id := fmt.Sprintf("demo-%d", at.UnixNano())
		return fmt.Sprintf(`curl -fsS '%s/api/v1/events' \
  -H 'Content-Type: application/json' \
  -d '{
    "event_id": "%s",
    "type": "span.complete",
    "run_id": "%s",
    "span_id": "%s",
    "kind": "llm",
    "name": "demo call",
    "time": "%s",
    "status": "ok",
    "model": "gpt-4o-mini",
    "tokens_in": 12,
    "tokens_out": 8
  }'`, base, id, id, id, at.Format(time.RFC3339Nano))
	case 1:
		if base == "" {
			return ""
		}
		return "export OTEL_EXPORTER_OTLP_ENDPOINT=" + base + "\nexport OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf"
	case 2:
		if base == "" {
			return ""
		}
		return "pip install capybara-sdk\n# in app.py: import capybara; capybara.init()\nOTEL_EXPORTER_OTLP_ENDPOINT=" + base + " python app.py"
	case 3:
		if base == "" {
			return ""
		}
		return "# Node SDK is not published on npm yet. Build from a capybara checkout:\n" +
			"npm ci --prefix /path/to/capybara/sdk-js\n" +
			"npm run build --prefix /path/to/capybara/sdk-js\n" +
			"# In your app: npm install /path/to/capybara/sdk-js\n" +
			"# In app.js: import { init } from 'capybara-sdk'; init()\n" +
			"OTEL_EXPORTER_OTLP_ENDPOINT=" + base + " node app.js"
	case 4:
		return "capybara watch claude"
	default:
		return "capybara import trace.jsonl"
	}
}

func (m appModel) connectView() string {
	var lines []string
	lines = append(lines, m.th.PaneTitle.Render("Connect  (Esc to close)"), "")
	if m.about.OTLP == "" {
		lines = append(lines, m.th.StatusErr.Render("HTTP listener unavailable"))
		if m.about.OTLPErr != "" {
			lines = append(lines, m.th.Dim.Render(m.about.OTLPErr))
		}
	} else {
		lines = append(lines, m.th.Accent.Render(m.about.OTLP+"/api/v1/events"))
	}
	policy := "content capture: on"
	if !m.capture {
		policy = "content capture: off (metadata only)"
	}
	lines = append(lines, m.th.Dim.Render(policy))
	if m.connectRuns > 0 {
		lines = append(lines, m.th.Accent.Render(fmt.Sprintf("HTTP event runs stored: %d", m.connectRuns)))
	} else if m.about.OTLP != "" {
		lines = append(lines, m.th.Dim.Render("Waiting for the first HTTP event"))
	}
	if m.notice != "" {
		lines = append(lines, m.th.Dim.Render(m.notice))
	}
	lines = append(lines, "")
	for i, option := range connectOptions {
		prefix := "  "
		if i == m.connectIndex {
			prefix = "> "
		}
		line := prefix + option
		if i == m.connectIndex {
			lines = append(lines, m.th.Accent.Render(line))
		} else {
			lines = append(lines, m.th.Text.Render(line))
		}
	}
	lines = append(lines, "")
	lines = append(lines, m.th.Dim.Render(connectDescriptions[m.connectIndex]), "")
	if snippet := m.connectSnippet(); snippet != "" {
		lines = append(lines, m.th.Dim.Render("Enter/y: send copy request  ·  j/k: select"))
		for _, line := range strings.Split(snippet, "\n") {
			lines = append(lines, m.th.Text.Render(line))
		}
	}
	content := strings.Join(lines, "\n")
	return m.headerView() + "\n" + lipgloss.NewStyle().Width(max(1, m.width-2)).MaxHeight(max(1, m.height-2)).Render(content)
}
