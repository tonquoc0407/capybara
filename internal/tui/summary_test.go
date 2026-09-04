package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tonquoc0407/capybara/internal/store"
	"github.com/tonquoc0407/capybara/internal/theme"
)

// modelWithRuns drives the real message order: the window is sized before the
// runs arrive, which is what once left the list stuck at one visible item.
func modelWithRuns(t *testing.T, n int) appModel {
	t.Helper()
	st := seededStore(t)
	base := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)
	for i := 1; i < n; i++ {
		id := string(rune('a'+i)) + "000000000000000"
		batch := store.Batch{
			Source: "test",
			Spans: []store.Span{{
				ID: id + "-root", RunID: id, Kind: store.KindAgent, Name: "agent_loop",
				StartedAt: base.Add(time.Duration(i) * time.Hour),
				EndedAt:   base.Add(time.Duration(i)*time.Hour + time.Second), Status: "ok",
			}},
		}
		if err := st.WriteBatch(context.Background(), batch); err != nil {
			t.Fatalf("WriteBatch: %v", err)
		}
	}
	ch, cancel := st.Subscribe()
	t.Cleanup(cancel)
	m := newApp(st, theme.Bara(), ch, true)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(appModel)
	runs, err := st.ListRuns(context.Background())
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != n {
		t.Fatalf("seeded %d runs, want %d", len(runs), n)
	}
	next, _ = m.Update(runsMsg{runs: runs})
	return next.(appModel)
}

func TestRunListShowsEveryRun(t *testing.T) {
	m := modelWithRuns(t, 3)
	view := plain(m.runs.view())
	for _, r := range []string{"b0000000", "c0000000", "r1"} {
		if !strings.Contains(view, r) {
			t.Errorf("run list is missing %q:\n%s", r, view)
		}
	}
}

func TestSummaryNamesWhatTheRunWas(t *testing.T) {
	m := modelWithRuns(t, 2)
	m.spans, _ = m.st.Spans(context.Background(), m.selectedRun)
	view := plain(m.summaryView(30))
	for _, want := range []string{"started", "source", "spans"} {
		if !strings.Contains(view, want) {
			t.Errorf("summary missing %q:\n%s", want, view)
		}
	}
}

func TestCompactCountKeepsNarrowPanesReadable(t *testing.T) {
	cases := map[int64]string{0: "0", 999: "999", 1500: "1.5k", 49160: "49k", 2_400_000: "2.4M"}
	for n, want := range cases {
		if got := compactCount(n); got != want {
			t.Errorf("compactCount(%d) = %q, want %q", n, got, want)
		}
	}
}

// parse_error and orphaned_span hang off no span, so counting only the
// per-span map reported "no findings" on a run that had them.
func TestRunSummaryCountsRunLevelFindings(t *testing.T) {
	m := newApp(seededStore(t), theme.Bara(), nil, true)
	m.width, m.height = 110, 32
	m.layout()
	m.runFindings = []store.Finding{
		{RunID: "r1", Type: "orphaned_span", Severity: "error"},
	}
	lines := strings.Join(m.findingLines(30), "\n")
	if strings.Contains(lines, "no findings") {
		t.Errorf("summary said there were none:\n%s", lines)
	}
	if !strings.Contains(lines, "1 orphaned_span") {
		t.Errorf("summary missing the run-level finding:\n%s", lines)
	}
}
