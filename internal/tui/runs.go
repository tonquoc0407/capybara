package tui

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tonquoc0407/capybara/internal/store"
	"github.com/tonquoc0407/capybara/internal/theme"
)

type runItem struct {
	run store.Run
}

// FilterValue feeds the list bubble's built-in `/` filter.
func (i runItem) FilterValue() string {
	return i.run.ID + " " + i.run.Label + " " + i.run.ModelMain
}

type runDelegate struct {
	th    theme.Theme
	width *int
}

// The delegate's own geometry; the layout needs it to size the list pane to its
// contents.
const (
	runItemHeight  = 2
	runItemSpacing = 1
	// Two borders, the pane title, and the row the list keeps for its filter.
	runListChrome = 4
)

func (d runDelegate) Height() int                         { return runItemHeight }
func (d runDelegate) Spacing() int                        { return runItemSpacing }
func (d runDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }
func (d runDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(runItem)
	if !ok {
		return
	}
	label := it.run.Label
	if label == "" {
		label = shortID(it.run.ID)
	}
	width := max(1, *d.width)
	style := d.th.Text
	switch {
	case index == m.Index():
		style = d.th.Selected
	case it.run.Status == "error":
		style = d.th.StatusErr
	case it.run.Findings > 0:
		style = d.th.StatusWarn
	case it.run.Status == "running":
		style = d.th.StatusRun
	}
	head := style.Render(truncate(runMark(it.run)+" "+label, width))
	fmt.Fprint(w, head+"\n"+d.th.Dim.Render(truncate("  "+runMeta(it.run), width)))
}

func runMark(r store.Run) string {
	switch {
	case r.Status == "error":
		return "x"
	case r.Findings > 0:
		return "!"
	case r.Status == "running":
		return "."
	}
	return " "
}

// runMeta is the second line: enough to tell two runs apart without opening
// either of them.
func runMeta(r store.Run) string {
	parts := []string{}
	if !r.StartedAt.IsZero() {
		parts = append(parts, r.StartedAt.Local().Format("15:04"))
	}
	if d := r.EndedAt.Sub(r.StartedAt); d > 0 && !r.StartedAt.IsZero() {
		parts = append(parts, humanDuration(d.Seconds()))
	}
	if r.CostUSD != nil {
		parts = append(parts, fmt.Sprintf("$%.2f", *r.CostUSD))
	}
	// Last, because it is the first thing worth losing to truncation: two runs
	// of the same agent almost always share a model.
	if r.ModelMain != "" {
		parts = append(parts, shortModel(r.ModelMain))
	}
	return strings.Join(parts, " ")
}

// shortModel drops the vendor prefix and release stamp: claude-opus-4-8 reads
// as opus-4-8 in a pane this narrow.
func shortModel(model string) string {
	for _, prefix := range []string{"claude-", "gpt-", "gemini-", "models/"} {
		model = strings.TrimPrefix(model, prefix)
	}
	if i := strings.LastIndex(model, "-2"); i > 0 && len(model)-i >= 7 {
		model = model[:i]
	}
	return model
}

func humanDuration(seconds float64) string {
	return humanDur(time.Duration(seconds * float64(time.Second)))
}

type runsModel struct {
	th    theme.Theme
	list  list.Model
	width int
}

func newRuns(th theme.Theme) *runsModel {
	m := &runsModel{th: th}
	l := list.New(nil, runDelegate{th: th, width: &m.width}, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(true)
	l.DisableQuitKeybindings()
	m.list = l
	return m
}

func (m *runsModel) setSize(w, h int) {
	m.width = w
	m.list.SetSize(w, h)
}

func (m *runsModel) setRuns(runs []store.Run) {
	sel := m.selectedID()
	items := make([]list.Item, len(runs))
	idx := -1
	for i, r := range runs {
		items[i] = runItem{run: r}
		if r.ID == sel {
			idx = i
		}
	}
	m.list.SetItems(items)
	if idx >= 0 {
		m.list.Select(idx)
	} else if len(items) > 0 {
		m.list.Select(0)
	}
}

func (m *runsModel) selectedID() string {
	if it, ok := m.list.SelectedItem().(runItem); ok {
		return it.run.ID
	}
	return ""
}

func (m *runsModel) selectedRun() (store.Run, bool) {
	it, ok := m.list.SelectedItem().(runItem)
	return it.run, ok
}

func (m *runsModel) typing() bool {
	return m.list.FilterState() == list.Filtering
}

func (m *runsModel) update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return cmd
}

func (m *runsModel) view() string {
	if len(m.list.Items()) == 0 {
		return m.th.Dim.Render("no runs yet")
	}
	return m.list.View()
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
