package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	reflowwrap "github.com/muesli/reflow/wrap"

	"github.com/tonquoc0407/capybara/internal/store"
)

var (
	connectOptions      = []string{"HTTP JSON events", "OTLP", "Python SDK", "Node SDK (source build)", "Claude watcher", "File import"}
	connectDescriptions = []string{
		"Send spans from a local app without an SDK.",
		"Point an instrumented app at the local OTLP receiver.",
		"Add tracing to Python code and export it locally.",
		"Build the Node SDK from this repository, then instrument your app.",
		"Read Claude Code sessions already stored on this machine.",
		"Load a saved trace file into this database.",
	}
)

func (m appModel) handleConnectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.connectSetupOpen {
		switch msg.String() {
		case "esc", "v":
			m.connectSetupOpen = false
		case "up", "k":
			m.connectScroll = max(0, m.connectScroll-1)
		case "down", "j":
			m.connectScroll++
		case "pgup", "ctrl+u":
			m.connectScroll = max(0, m.connectScroll-max(1, m.height/2))
		case "pgdown", "ctrl+d":
			m.connectScroll += max(1, m.height/2)
		case "y", "enter":
			m.copyConnectSnippet()
		case "o":
			return m.openConnectedRun()
		}
		return m, nil
	}
	switch msg.String() {
	case "esc", "q", "i":
		m.connectOpen = false
	case "up", "k":
		m.connectIndex = (m.connectIndex + len(connectOptions) - 1) % len(connectOptions)
		m.connectScroll = 0
		m.notice = ""
	case "down", "j":
		m.connectIndex = (m.connectIndex + 1) % len(connectOptions)
		m.connectScroll = 0
		m.notice = ""
	case "y", "enter":
		m.copyConnectSnippet()
	case "v":
		m.connectSetupOpen = true
		m.connectScroll = 0
	case "o":
		return m.openConnectedRun()
	}
	return m, nil
}

func (m *appModel) copyConnectSnippet() {
	if snippet := m.connectSnippet(); snippet != "" {
		if err := copyToClipboard(snippet); err != nil {
			m.notice = "clipboard unavailable; press v to read the full setup"
		} else {
			m.notice = "copy command sent; paste to confirm"
		}
	}
}

func (m appModel) openConnectedRun() (tea.Model, tea.Cmd) {
	if m.connectNewRun != "" && m.runs.selectByID(m.connectNewRun) {
		if run, ok := m.runs.selectedRun(); ok {
			m.detail.setRun(run)
		}
		m.detail.setSpan(store.Span{}, nil, nil, nil)
		m.selectedRun = m.connectNewRun
		m.connectOpen = false
		m.connectSetupOpen = false
		m.mode = viewTree
		m.focus = focusTree
		m.layout()
		return m, m.loadSpans(m.selectedRun)
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
	if m.connectSetupOpen {
		return m.connectSetupView()
	}
	if m.th.Name == "pupu" {
		return m.pupuConnectView()
	}
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
	if m.connectNewRun != "" {
		lines = append(lines, m.th.StatusOK.Render("Trace received: "+m.connectNewRun+"  ·  press o to open"))
	} else {
		lines = append(lines, m.th.Dim.Render("Waiting for a new trace  ·  run the setup, then watch here"))
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
		lines = append(lines, m.th.Dim.Render("Enter/y: copy setup  ·  o: open received trace  ·  j/k: select"))
		for _, line := range strings.Split(snippet, "\n") {
			lines = append(lines, m.th.Text.Render(line))
		}
	}
	content := strings.Join(lines, "\n")
	return m.headerView() + "\n" + lipgloss.NewStyle().Width(max(1, m.width-2)).MaxHeight(max(1, m.height-2)).Render(content)
}

func (m appModel) connectSetupView() string {
	header := m.headerView()
	footer := m.th.Shell.Width(m.width).Render(m.th.HelpKey.Render("j/k") + " scroll   " +
		m.th.HelpKey.Render("enter") + " copy   " + m.th.HelpKey.Render("o") + " open trace   " + m.th.HelpKey.Render("esc") + " sources")
	areaH := max(3, m.height-lipgloss.Height(header)-lipgloss.Height(footer))
	width := max(1, m.width-paneChrome)
	lines := []string{m.th.Accent.Render(connectOptions[m.connectIndex]), m.th.Dim.Render(connectDescriptions[m.connectIndex]), ""}
	for _, line := range strings.Split(m.connectSnippet(), "\n") {
		lines = append(lines, reflowwrap.String(stripControl(line), width))
	}
	if m.connectNewRun != "" {
		lines = append(lines, "", m.th.StatusOK.Render("Trace received: "+m.connectNewRun+"  ·  o to open"))
	}
	if m.notice != "" {
		lines = append(lines, "", m.th.Dim.Render(m.notice))
	}
	// Reflow can add rows inside one source line; scroll by actual screen row.
	content := strings.Split(strings.Join(lines, "\n"), "\n")
	visible := max(1, areaH-3)
	start := min(max(0, m.connectScroll), max(0, len(content)-visible))
	end := min(len(content), start+visible)
	return header + "\n" + m.pane("setup  ·  "+fmt.Sprintf("%d/%d", start+1, max(1, len(content))), strings.Join(content[start:end], "\n"), m.width, areaH, true) + "\n" + footer
}

func (m appModel) pupuConnectView() string {
	header := m.headerView()
	footer := m.th.Shell.Width(m.width).Render(m.th.HelpKey.Render("j/k") + " choose   " +
		m.th.HelpKey.Render("v") + " full setup   " + m.th.HelpKey.Render("enter") + " copy   " + m.th.HelpKey.Render("o") + " open")
	areaH := max(3, m.height-lipgloss.Height(header)-lipgloss.Height(footer))
	leftW := min(34, max(24, m.width/3))
	var options []string
	for i, option := range connectOptions {
		row := fmt.Sprintf(" %d  %s", i+1, option)
		if i == m.connectIndex {
			options = append(options, m.th.Selected.Width(max(1, leftW-paneChrome)).Render(row))
		} else {
			options = append(options, m.th.Text.Render(row))
		}
	}
	status := "● HTTP ready"
	if m.about.OTLP == "" {
		status = "! HTTP unavailable"
	}
	right := []string{m.th.Accent.Render(status), ""}
	if m.about.OTLP != "" {
		right = append(right, m.th.Dim.Render(m.about.OTLP+"/api/v1/events"))
	} else if m.about.OTLPErr != "" {
		right = append(right, m.th.StatusErr.Render(m.about.OTLPErr))
	}
	policy := "content capture on"
	if !m.capture {
		policy = "content capture off"
	}
	right = append(right, m.th.Dim.Render(fmt.Sprintf("%s  ·  %d HTTP runs", policy, m.connectRuns)), "", m.th.Text.Render(connectDescriptions[m.connectIndex]))
	if m.connectNewRun != "" {
		right = append(right, "", m.th.StatusOK.Render("Trace received: "+m.connectNewRun), m.th.Accent.Render("Press o to open it"))
	} else {
		right = append(right, "", m.th.Dim.Render("Waiting for a new trace"))
	}
	if m.notice != "" {
		right = append(right, "", m.th.Accent.Render(m.notice))
	}
	if snippet := m.connectSnippet(); snippet != "" {
		right = append(right, "", m.th.PaneTitle.Render("COPYABLE SETUP"))
		for _, line := range strings.Split(snippet, "\n") {
			right = append(right, m.th.Text.Render(line))
		}
	}
	if m.width < 72 {
		content := append([]string{}, options...)
		content = append(content, "", m.th.Accent.Render(status), m.th.Dim.Render(policy),
			m.th.Text.Render(connectDescriptions[m.connectIndex]), "",
			m.th.Dim.Render("v shows the full setup; Enter copies it"))
		if m.connectNewRun != "" {
			content = append(content, m.th.StatusOK.Render("Received "+shortID(m.connectNewRun)+" · o to open"))
		}
		if len(content) > areaH-3 {
			content = content[:areaH-3]
		}
		return header + "\n" + m.pane("connect", strings.Join(content, "\n"), m.width, areaH, true) + "\n" + footer
	}
	rightW := m.width - leftW - paneGap
	if len(right) > areaH-3 {
		right = right[:areaH-3]
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		m.pane("sources", strings.Join(options, "\n"), leftW, areaH, true),
		lipgloss.NewStyle().Width(paneGap).Height(areaH).Render(""),
		m.pane("setup", strings.Join(right, "\n"), rightW, areaH, false))
	return header + "\n" + body + "\n" + footer
}
