package tui

import (
	"fmt"
	"io"

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

func (d runDelegate) Height() int                         { return 1 }
func (d runDelegate) Spacing() int                        { return 0 }
func (d runDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }
func (d runDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(runItem)
	if !ok {
		return
	}
	mark := " "
	switch {
	case it.run.Status == "error":
		mark = "x"
	case it.run.Findings > 0:
		mark = "!"
	case it.run.Status == "running":
		mark = "."
	}
	label := it.run.Label
	if label == "" {
		label = shortID(it.run.ID)
	}
	plain := truncate(fmt.Sprintf("%s %s", mark, label), max(1, *d.width))
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
	fmt.Fprint(w, style.Render(plain))
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
