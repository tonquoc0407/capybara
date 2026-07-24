package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/export"
	"github.com/tonquoc0407/capybara/internal/store"
	"github.com/tonquoc0407/capybara/internal/theme"
)

type focusArea int

const (
	focusRuns focusArea = iota
	focusTree
	focusDetail
	focusCount
)

// viewMode selects what the middle pane shows; all modes share the shell.
type viewMode int

const (
	viewTree viewMode = iota
	viewWaterfall
	viewContext
	viewDiff
	viewBlame
)

type (
	refreshMsg struct{}
	runsMsg    []store.Run
	spansMsg   struct {
		runID    string
		spans    []store.Span
		findings map[string][]store.Finding
	}
	contentsMsg struct {
		span     store.Span
		contents []store.Content
		findings []store.Finding
	}
	statsMsg struct {
		runID string
		stats map[string]map[string]int64
	}
	diffMsg   struct{ diff *analyze.RunDiff }
	blameMsg  struct{ chain *analyze.BlameChain }
	exportMsg struct {
		paths []string
		err   error
	}
	errMsg struct{ err error }
)

type keyMap struct {
	Nav    key.Binding
	Expand key.Binding
	Panes  key.Binding
	Search key.Binding
	Filter key.Binding
	Views  key.Binding
	Diff   key.Binding
	Blame  key.Binding
	Test   key.Binding
	Edit   key.Binding
	Rerun  key.Binding
	Attrs  key.Binding
	Help   key.Binding
	Quit   key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Nav:    key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k", "nav")),
		Expand: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "expand")),
		Panes:  key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "panes")),
		Search: key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		Filter: key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "filter")),
		Views:  key.NewBinding(key.WithKeys("w", "c"), key.WithHelp("w/c", "waterfall/context")),
		Diff:   key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "diff")),
		Blame:  key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "blame")),
		Test:   key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "export test")),
		Edit:   key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit output")),
		Rerun:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "re-run")),
		Attrs:  key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "attrs")),
		Help:   key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Nav, k.Expand, k.Search, k.Filter, k.Views, k.Diff, k.Blame, k.Rerun, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Nav, k.Expand, k.Panes},
		{k.Search, k.Filter, k.Attrs},
		{k.Views, k.Diff, k.Blame},
		{k.Test, k.Edit, k.Rerun},
		{k.Help, k.Quit},
	}
}

type appModel struct {
	st          *store.Store
	th          theme.Theme
	events      <-chan struct{}
	keys        keyMap
	help        help.Model
	runs        *runsModel
	tree        treeModel
	waterfall   waterfallModel
	contextv    contextModel
	diffv       diffModel
	blamev      blameModel
	detail      detailModel
	mode        viewMode
	focus       focusArea
	width       int
	height      int
	selectedRun string
	diffMark    string
	edit        edit
	capture     bool
	replaying   bool
	splash      bool
	version     string
	dbPath      string
	about       About
	spans       []store.Span
	findings    map[string][]store.Finding
	notice      string
	lastErr     error
}

func newApp(st *store.Store, th theme.Theme, events <-chan struct{}, captureContent bool) appModel {
	h := help.New()
	h.Styles.ShortKey = th.HelpKey
	h.Styles.ShortDesc = th.HelpDesc
	h.Styles.ShortSeparator = th.HelpDesc
	h.Styles.FullKey = th.HelpKey
	h.Styles.FullDesc = th.HelpDesc
	h.Styles.FullSeparator = th.HelpDesc
	return appModel{
		st:        st,
		th:        th,
		events:    events,
		keys:      defaultKeys(),
		help:      h,
		runs:      newRuns(th),
		tree:      newTree(th),
		waterfall: newWaterfall(th),
		contextv:  newContext(th),
		diffv:     newDiff(th),
		blamev:    newBlame(th),
		detail:    newDetail(th),
		focus:     focusTree,
		capture:   captureContent,
		splash:    true,
	}
}

func (m appModel) Init() tea.Cmd {
	hold := tea.Tick(splashHold, func(time.Time) tea.Msg { return splashDoneMsg{} })
	return tea.Batch(m.loadRuns(), m.listen(), hold)
}

func (m appModel) listen() tea.Cmd {
	ch := m.events
	return func() tea.Msg {
		if _, ok := <-ch; !ok {
			return nil
		}
		return refreshMsg{}
	}
}

func (m appModel) loadRuns() tea.Cmd {
	st := m.st
	return func() tea.Msg {
		runs, err := st.ListRuns(context.Background())
		if err != nil {
			return errMsg{err}
		}
		return runsMsg(runs)
	}
}

func (m appModel) loadSpans(runID string) tea.Cmd {
	st := m.st
	return func() tea.Msg {
		spans, err := st.Spans(context.Background(), runID)
		if err != nil {
			return errMsg{err}
		}
		findings, err := st.Findings(context.Background(), runID)
		if err != nil {
			return errMsg{err}
		}
		bySpan := make(map[string][]store.Finding)
		for _, f := range findings {
			if f.SpanID != "" {
				bySpan[f.SpanID] = append(bySpan[f.SpanID], f)
			}
		}
		return spansMsg{runID: runID, spans: spans, findings: bySpan}
	}
}

func (m appModel) loadStats(runID string) tea.Cmd {
	st := m.st
	return func() tea.Msg {
		stats, err := st.ContentStats(context.Background(), runID)
		if err != nil {
			return errMsg{err}
		}
		return statsMsg{runID: runID, stats: stats}
	}
}

func (m appModel) loadContents(sp store.Span) tea.Cmd {
	st := m.st
	findings := m.findings[sp.ID]
	return func() tea.Msg {
		contents, err := st.Contents(context.Background(), sp.ID)
		if err != nil {
			return errMsg{err}
		}
		return contentsMsg{span: sp, contents: contents, findings: findings}
	}
}

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil
	case splashDoneMsg:
		m.splash = false
		return m, nil
	case refreshMsg:
		cmds := []tea.Cmd{m.loadRuns(), m.listen()}
		if m.selectedRun != "" {
			cmds = append(cmds, m.loadSpans(m.selectedRun))
		}
		return m, tea.Batch(cmds...)
	case runsMsg:
		m.runs.setRuns(msg)
		m.layout() // the run column is sized from the item count, which just changed
		if sel := m.runs.selectedID(); sel != m.selectedRun {
			m.selectedRun = sel
			return m, m.loadSpans(sel)
		}
		return m, nil
	case spansMsg:
		if msg.runID != m.selectedRun {
			return m, nil
		}
		m.spans = msg.spans
		m.findings = msg.findings
		m.tree.setSpans(msg.spans, msg.findings)
		m.waterfall.setSpans(msg.spans)
		cmds := []tea.Cmd{}
		if m.mode == viewContext {
			cmds = append(cmds, m.loadStats(msg.runID))
		}
		if sp, ok := m.middleSelected(); ok {
			cmds = append(cmds, m.loadContents(sp))
		} else {
			m.detail.setSpan(store.Span{}, nil, nil)
		}
		return m, tea.Batch(cmds...)
	case statsMsg:
		if msg.runID == m.selectedRun {
			m.contextv.setData(m.spans, msg.stats)
		}
		return m, nil
	case diffMsg:
		m.diffv.setDiff(msg.diff)
		m.mode = viewDiff
		m.focus = focusTree
		m.diffMark = ""
		m.layout()
		m.syncDiffDetail()
		return m, nil
	case blameMsg:
		m.blamev.setChain(msg.chain)
		m.mode = viewBlame
		m.focus = focusTree
		m.layout()
		if sp, ok := m.blamev.selected(); ok {
			return m, m.loadContents(sp)
		}
		return m, nil
	case exportMsg:
		if msg.err != nil {
			m.lastErr = msg.err
		} else if len(msg.paths) > 0 {
			m.notice = "wrote " + msg.paths[len(msg.paths)-1]
		}
		return m, nil
	case editReadyMsg:
		return m, m.editorCmd(msg)
	case editDoneMsg:
		return m.applyEdit(msg)
	case replayDoneMsg:
		return m.finishReplay(msg)
	case contentsMsg:
		if sel, ok := m.tree.selected(); ok && sel.ID == msg.span.ID {
			m.detail.setSpan(msg.span, msg.contents, msg.findings)
		}
		return m, nil
	case errMsg:
		m.lastErr = msg.err
		return m, nil
	case tea.KeyMsg:
		if m.splash && !key.Matches(msg, m.keys.Quit) {
			m.splash = false
			return m, nil
		}
		return m.handleKey(msg)
	}
	return m, nil
}

func (m appModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	typing := (m.focus == focusTree && m.mode == viewTree && m.tree.typing()) ||
		(m.focus == focusRuns && m.runs.typing())
	if !typing {
		m.notice = ""
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = !m.help.ShowAll
			m.layout()
			return m, nil
		case msg.String() == "tab":
			m.focus = (m.focus + 1) % focusCount
			return m, nil
		case msg.String() == "shift+tab":
			m.focus = (m.focus + focusCount - 1) % focusCount
			return m, nil
		case msg.String() == "w" && m.focus != focusDetail:
			return m.switchMode(viewWaterfall)
		case msg.String() == "c" && m.focus != focusDetail:
			return m.switchMode(viewContext)
		case msg.String() == "d" && m.focus != focusDetail:
			return m.handleDiffKey()
		case msg.String() == "b" && m.focus != focusDetail:
			return m.handleBlameKey()
		case msg.String() == "t" && m.focus != focusDetail:
			return m.handleTestKey()
		case msg.String() == "e" && m.focus != focusDetail:
			return m.handleEditKey()
		case msg.String() == "r" && m.focus != focusDetail && !m.replaying:
			return m.handleReplayKey()
		case msg.String() == "esc" && m.focus == focusTree && m.mode != viewTree:
			return m.switchMode(m.mode) // toggling the current mode returns to the tree
		}
	}
	switch m.focus {
	case focusRuns:
		cmd := m.runs.update(msg)
		if sel := m.runs.selectedID(); sel != m.selectedRun {
			m.selectedRun = sel
			return m, tea.Batch(cmd, m.loadSpans(sel))
		}
		return m, cmd
	case focusTree:
		return m.updateMiddle(msg)
	case focusDetail:
		return m, m.detail.update(msg)
	}
	return m, nil
}

// handleDiffKey implements the two-step diff: d marks the first run, a second
// d on another run compares them; d inside the diff leaves it.
func (m appModel) handleDiffKey() (tea.Model, tea.Cmd) {
	switch {
	case m.mode == viewDiff:
		m.mode = viewTree
		m.diffMark = ""
		m.layout()
		if sp, ok := m.middleSelected(); ok {
			return m, m.loadContents(sp)
		}
		return m, nil
	case m.selectedRun == "":
		return m, nil
	case m.diffMark == "" || m.diffMark == m.selectedRun:
		if m.diffMark == m.selectedRun {
			m.diffMark = ""
		} else {
			m.diffMark = m.selectedRun
		}
		return m, nil
	}
	st, a, b := m.st, m.diffMark, m.selectedRun
	return m, func() tea.Msg {
		d, err := analyze.DiffRuns(context.Background(), st, a, b)
		if err != nil {
			return errMsg{err}
		}
		return diffMsg{diff: d}
	}
}

// handleBlameKey toggles the blame view for the selected run, computed off the
// UI thread.
func (m appModel) handleBlameKey() (tea.Model, tea.Cmd) {
	if m.mode == viewBlame {
		m.mode = viewTree
		m.layout()
		if sp, ok := m.middleSelected(); ok {
			return m, m.loadContents(sp)
		}
		return m, nil
	}
	if m.selectedRun == "" {
		return m, nil
	}
	st, run := m.st, m.selectedRun
	return m, func() tea.Msg {
		chain, err := analyze.Blame(context.Background(), st, run)
		if err != nil {
			return errMsg{err}
		}
		return blameMsg{chain: chain}
	}
}

// handleTestKey exports a pytest regression case for the selected tool span,
// or for the whole run when the selection is not a tool call.
func (m appModel) handleTestKey() (tea.Model, tea.Cmd) {
	if m.selectedRun == "" {
		return m, nil
	}
	st, run, span := m.st, m.selectedRun, ""
	if sp, ok := m.middleSelected(); ok && sp.Kind == store.KindTool {
		span = sp.ID
	}
	return m, func() tea.Msg {
		fx, err := buildExportFixture(st, run, span)
		if err != nil {
			return exportMsg{err: err}
		}
		paths, err := export.WritePytest(export.DefaultDir, fx)
		return exportMsg{paths: paths, err: err}
	}
}

func buildExportFixture(st *store.Store, run, span string) (export.Fixture, error) {
	if span == "" {
		return export.BuildFixture(context.Background(), st, run)
	}
	return export.BuildSpanFixture(context.Background(), st, run, span)
}

// syncDiffDetail mirrors the selected diff step into the detail pane.
func (m *appModel) syncDiffDetail() {
	step, ok := m.diffv.selected()
	if !ok || m.diffv.diff == nil {
		return
	}
	d := diffDetail{
		name: step.StepName(),
		runA: m.diffv.diff.RunA, runB: m.diffv.diff.RunB,
		hasA: step.A != nil, hasB: step.B != nil,
	}
	if step.A != nil {
		d.sideA = m.diffv.diff.ContentsA[step.A.ID]
	}
	if step.B != nil {
		d.sideB = m.diffv.diff.ContentsB[step.B.ID]
	}
	m.detail.setDiffStep(d)
}

// switchMode toggles the middle pane between the tree and the given view.
func (m appModel) switchMode(mode viewMode) (tea.Model, tea.Cmd) {
	if m.mode == mode {
		m.mode = viewTree
	} else {
		m.mode = mode
	}
	m.focus = focusTree
	m.layout()
	cmds := []tea.Cmd{}
	if m.mode == viewContext && m.selectedRun != "" {
		cmds = append(cmds, m.loadStats(m.selectedRun))
	}
	if sp, ok := m.middleSelected(); ok {
		cmds = append(cmds, m.loadContents(sp))
	}
	return m, tea.Batch(cmds...)
}

// updateMiddle routes a key to whichever view occupies the middle pane.
func (m appModel) updateMiddle(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mode == viewDiff {
		m.diffv.update(msg)
		m.syncDiffDetail()
		return m, nil
	}
	before, _ := m.middleSelected()
	var cmd tea.Cmd
	switch m.mode {
	case viewTree:
		cmd = m.tree.update(msg)
	case viewWaterfall:
		m.waterfall.update(msg)
	case viewContext:
		m.contextv.update(msg)
	case viewBlame:
		m.blamev.update(msg)
	}
	if sp, ok := m.middleSelected(); ok && sp.ID != before.ID {
		return m, tea.Batch(cmd, m.loadContents(sp))
	}
	return m, cmd
}

func (m appModel) middleSelected() (store.Span, bool) {
	switch m.mode {
	case viewWaterfall:
		return m.waterfall.selected()
	case viewContext:
		return m.contextv.selected()
	case viewBlame:
		return m.blamev.selected()
	}
	return m.tree.selected()
}

// paneWidths splits the row: a narrow run column, then the middle and detail
// panes sharing what is left.
func (m appModel) paneWidths() (runs, middle, detail int) {
	runs = min(max(m.width/5, 18), 34)
	middle = (m.width - runs) * 52 / 100
	return runs, middle, m.width - runs - middle
}

// paneChrome is what a pane spends on itself: two border columns and a column
// of padding on each side, so text never touches the frame.
const paneChrome = 4

// summaryMinHeight is what the run summary needs before it is worth drawing:
// borders, title and the six or so fields it carries.
const summaryMinHeight = 12

// paneHeights stacks the run list over its summary. The list only claims the
// rows its items need, so a two-run database does not leave half the column
// empty.
func (m appModel) paneHeights(total int) (list, summary int) {
	// The list bubble sizes itself in whole item+spacing units and keeps a row
	// back for the filter prompt, so both have to be paid for or the last run
	// is scrolled out of view.
	needed := len(m.runs.list.Items())*(runItemHeight+runItemSpacing) + runListChrome
	list = min(max(needed, 8), max(8, total-summaryMinHeight))
	if list > total-3 {
		list = total
	}
	return list, total - list
}

func (m *appModel) layout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	m.help.Width = m.width
	statusH := lipgloss.Height(m.statusView())
	paneH := max(3, m.height-1-statusH)
	runsW, middleW, detailW := m.paneWidths()
	listH, _ := m.paneHeights(paneH)
	// -2 borders, -1 pane title line
	m.runs.setSize(runsW-paneChrome, listH-3)
	m.tree.setSize(middleW-paneChrome, paneH-3)
	m.waterfall.setSize(middleW-paneChrome, paneH-3)
	m.contextv.setSize(middleW-paneChrome, paneH-3)
	m.diffv.setSize(middleW-paneChrome, paneH-3)
	m.blamev.setSize(middleW-paneChrome, paneH-3)
	m.detail.setSize(detailW-paneChrome, paneH-3)
}

func (m appModel) View() string {
	if m.width <= 0 {
		return ""
	}
	if m.splash {
		return m.splashView()
	}
	statusView := m.statusView()
	paneH := max(3, m.height-1-lipgloss.Height(statusView))
	runsW, middleW, detailW := m.paneWidths()
	listH, summaryH := m.paneHeights(paneH)
	middleTitle, middleView := "span tree", m.tree.view()
	if len(m.runs.list.Items()) == 0 {
		middleTitle, middleView = "getting started", m.waitingView(middleW-paneChrome)
	}
	switch m.mode {
	case viewWaterfall:
		middleTitle, middleView = "cost waterfall", m.waterfall.view()
	case viewContext:
		middleTitle, middleView = "context", m.contextv.view()
	case viewDiff:
		if m.diffv.diff != nil {
			middleTitle = fmt.Sprintf("diff %s vs %s",
				shortID(m.diffv.diff.RunA), shortID(m.diffv.diff.RunB))
		}
		middleView = m.diffv.view()
	case viewBlame:
		middleTitle = "blame " + shortID(m.selectedRun)
		middleView = m.blamev.view()
	}
	left := m.pane("runs", m.runs.view(), runsW, listH, m.focus == focusRuns)
	if summaryH >= 3 {
		left = lipgloss.JoinVertical(lipgloss.Left, left,
			m.pane("run", m.summaryView(runsW-paneChrome), runsW, summaryH, false))
	}
	panes := lipgloss.JoinHorizontal(
		lipgloss.Top,
		left,
		m.pane(middleTitle, middleView, middleW, paneH, m.focus == focusTree),
		m.pane("detail", m.detail.view(), detailW, paneH, m.focus == focusDetail),
	)
	return m.headerView() + "\n" + panes + "\n" + statusView
}

func (m appModel) pane(title, content string, w, h int, focused bool) string {
	border := m.th.Border
	if focused {
		border = m.th.BorderFocus
	}
	inner := m.th.PaneTitle.Render(truncate(title, max(1, w-paneChrome))) + "\n" + content
	return border.
		Border(lipgloss.NormalBorder()).
		Padding(0, 1).
		Width(w - 2).
		Height(h - 2).
		Render(inner)
}

func (m appModel) headerView() string {
	title := m.th.Header.Render(" capybara ")
	info := ""
	if run, ok := m.runs.selectedRun(); ok {
		status := run.Status
		if status == "running" {
			status = "live"
		}
		info = fmt.Sprintf("run %s - %s", shortID(run.ID), status)
		if run.CostUSD != nil {
			info += fmt.Sprintf(" - $%.3f", *run.CostUSD)
		} else if run.TokensIn > 0 || run.TokensOut > 0 {
			info += fmt.Sprintf(" - tok %d/%d", run.TokensIn, run.TokensOut)
		}
		info = m.th.HeaderInfo.Render(info + " ")
	}
	fill := m.width - lipgloss.Width(title) - lipgloss.Width(info)
	return title + m.th.Border.Render(strings.Repeat("─", max(0, fill))) + info
}

func (m appModel) statusView() string {
	line := m.help.View(m.keys)
	if m.diffMark != "" && m.mode != viewDiff {
		line = m.th.Accent.Render("diff "+shortID(m.diffMark)+": select second run, press d") +
			m.th.StatusBar.Render(" | ") + line
	}
	if m.replaying {
		line = m.th.Accent.Render("replaying") + m.th.StatusBar.Render(" | ") + line
	} else if m.edit.spanID != "" {
		line = m.th.Accent.Render("edited "+m.edit.name+": press r to re-run") +
			m.th.StatusBar.Render(" | ") + line
	}
	if m.notice != "" {
		line = m.th.Accent.Render(truncate(m.notice, m.width/2)) +
			m.th.StatusBar.Render(" | ") + line
	}
	if run, ok := m.runs.selectedRun(); ok && run.Findings > 0 {
		seg := fmt.Sprintf("%d findings", run.Findings)
		if run.Findings == 1 {
			seg = "1 finding"
		}
		line = m.th.StatusWarn.Render(seg) + m.th.StatusBar.Render(" | ") + line
	}
	if m.lastErr != nil {
		line = m.th.StatusErr.Render(truncate(m.lastErr.Error(), m.width/2)) +
			m.th.StatusBar.Render(" | ") + line
	}
	return m.faceStyle().Render(theme.Face(m.mood())) + " " + line
}

func (m appModel) faceStyle() lipgloss.Style {
	switch m.mood() {
	case theme.Concerned:
		return m.th.StatusErr
	case theme.Alert:
		return m.th.StatusWarn
	default:
		return m.th.StatusBar
	}
}
