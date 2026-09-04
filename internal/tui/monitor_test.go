package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
	"github.com/tonquoc0407/capybara/internal/theme"
)

func reading(spanID, name string, at time.Time, cpu float64, rss int64) store.ResourceSample {
	return store.ResourceSample{
		SpanID: spanID, SpanName: name, At: at, CPUUtil: &cpu, RSSBytes: &rss,
	}
}

func testMonitor(samples []store.ResourceSample) monitorModel {
	m := newMonitor(theme.Bara())
	m.setSize(50, 24)
	m.setSamples(samples)
	return m
}

func TestMonitorSaysSoWithNoReadings(t *testing.T) {
	m := testMonitor(nil)
	if !strings.Contains(m.view(), "no readings yet") {
		t.Errorf("empty monitor = %q, want it to say there are none", m.view())
	}
}

func TestMonitorGraphsCPUAndMemory(t *testing.T) {
	m := testMonitor([]store.ResourceSample{
		reading("s1", "execute_tool parse", base, 1.0, 100<<20),
		reading("s1", "execute_tool parse", base.Add(time.Second), 0.5, 200<<20),
	})
	out := m.view()
	for _, want := range []string{"cpu", "memory", "200.0MB"} {
		if !strings.Contains(out, want) {
			t.Errorf("monitor missing %q:\n%s", want, out)
		}
	}
	if !strings.ContainsRune(out, '⣿') && !strings.ContainsRune(out, '⡇') {
		t.Errorf("monitor drew no braille:\n%s", out)
	}
}

// A run with no card must not show two empty gpu panels taking half the pane.
func TestMonitorHidesGPUWhenNothingReportedIt(t *testing.T) {
	m := testMonitor([]store.ResourceSample{
		reading("s1", "execute_tool parse", base, 0.5, 1<<20),
	})
	if strings.Contains(m.view(), "gpu") {
		t.Errorf("monitor showed a gpu section without any gpu reading:\n%s", m.view())
	}
}

func TestMonitorShowsGPUWhenPresent(t *testing.T) {
	util, mem := 0.25, int64(1<<30)
	sm := reading("s1", "execute_tool train", base, 0.5, 1<<20)
	sm.GPUUtil, sm.GPUMemBytes = &util, &mem
	out := testMonitor([]store.ResourceSample{sm}).view()
	if !strings.Contains(out, "gpu") || !strings.Contains(out, "25%") {
		t.Errorf("monitor missing the gpu reading:\n%s", out)
	}
}

// The operation prefix is identical on every node and only eats width.
func TestNodeLabelDropsTheOperationPrefix(t *testing.T) {
	cases := map[string]string{
		"execute_tool embed_corpus": "embed_corpus",
		"invoke_agent planner":      "planner",
		"chat gpt-4":                "gpt-4",
		"bare_name":                 "bare_name",
	}
	for in, want := range cases {
		got := nodeLabel(store.ResourceSample{SpanName: in})
		if got != want {
			t.Errorf("nodeLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

// A crash leaves readings whose span never arrived, so the id is all there is.
func TestNodeLabelFallsBackToTheSpanID(t *testing.T) {
	got := nodeLabel(store.ResourceSample{SpanID: "abcdef1234567890"})
	if got != "abcdef12" {
		t.Errorf("nodeLabel = %q, want the short id", got)
	}
}

func TestMonitorTimelineNamesEachNodeOnce(t *testing.T) {
	m := testMonitor([]store.ResourceSample{
		reading("s1", "execute_tool parse", base, 1, 1),
		reading("s1", "execute_tool parse", base.Add(time.Second), 1, 1),
		reading("s2", "execute_tool embed", base.Add(2*time.Second), 1, 1),
	})
	out := m.view()
	if strings.Count(out, "parse") != 1 {
		t.Errorf("a node repeated in the timeline:\n%s", out)
	}
	if !strings.Contains(out, "embed") {
		t.Errorf("timeline missing the second node:\n%s", out)
	}
}

// A gauge absent from one tick was not zero, it just was not read.
func TestSeriesHoldsTheLastValueThroughAGap(t *testing.T) {
	gap := store.ResourceSample{SpanID: "s1", At: base.Add(time.Second)}
	m := testMonitor([]store.ResourceSample{
		reading("s1", "execute_tool parse", base, 0.5, 100<<20),
		gap,
	})
	all := m.buildSeries()
	rss := all[1]
	if len(rss.values) != 2 || rss.values[1] != rss.values[0] {
		t.Errorf("series dropped to %v across a gap, want it held", rss.values)
	}
}
