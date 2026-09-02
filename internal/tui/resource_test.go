package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
	"github.com/tonquoc0407/capybara/internal/theme"
)

func TestResourceLineRendersCPUAsAPercentage(t *testing.T) {
	cpu, rss := 1.5, int64(3*1024*1024)
	got := resourceLine(&store.ResourceSample{CPUUtil: &cpu, RSSBytes: &rss})
	if got != "cpu 150% rss 3.0MB" {
		t.Errorf("resourceLine = %q, want cpu 150%% rss 3.0MB", got)
	}
}

func TestResourceLineIsEmptyWithoutASample(t *testing.T) {
	if got := resourceLine(nil); got != "" {
		t.Errorf("resourceLine(nil) = %q, want empty", got)
	}
}

func TestResourceLineSkipsAMissingReading(t *testing.T) {
	rss := int64(512)
	if got := resourceLine(&store.ResourceSample{RSSBytes: &rss}); got != "rss 512B" {
		t.Errorf("resourceLine = %q, want just the rss half", got)
	}
}

func TestDetailHeaderCarriesTheLastReading(t *testing.T) {
	m := newDetail(theme.Bara())
	m.setSize(70, 20)
	cpu, rss := 0.42, int64(9*1024*1024)
	m.setSpan(span("tool1", "root", store.KindTool, "embed", 1, 1), nil, nil,
		&store.ResourceSample{CPUUtil: &cpu, RSSBytes: &rss})
	out := plainView(m)
	if !strings.Contains(out, "cpu 42%") || !strings.Contains(out, "rss 9.0MB") {
		t.Errorf("detail header missing the reading:\n%s", out)
	}
}

// x already means the tool itself returned an error; a span nothing came back
// from is a different failure and must not read as the same one.
func TestTreeMarksAnOrphanedSpanApartFromAToolError(t *testing.T) {
	m := newTree(theme.Bara())
	m.setSize(60, 10)
	open := span("tool1", "", store.KindTool, "embed_corpus", 1, 1)
	open.EndedAt = time.Time{} // the span a crash left open never got an end
	m.setSpans([]store.Span{open}, map[string][]store.Finding{
		"tool1": {{RunID: "r1", SpanID: "tool1", Type: "orphaned_span", Severity: "error"}},
	})
	line := ansiRE.ReplaceAllString(m.renderRow(0), "")
	if !strings.Contains(line, "?") {
		t.Errorf("orphaned span line = %q, want the ? mark", line)
	}
	if strings.Contains(line, " x ") {
		t.Errorf("orphaned span line = %q, must not reuse the tool-error mark", line)
	}
	if !strings.Contains(line, "no end") {
		t.Errorf("orphaned span line = %q, want it to say the span never closed", line)
	}
}

func TestHumanBytesTiers(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{512, "512B"},
		{2048, "2.0KB"},
		{5 * 1024 * 1024, "5.0MB"},
		{3 * 1024 * 1024 * 1024, "3.0GB"},
	}
	for _, c := range cases {
		if got := humanBytes(c.n); got != c.want {
			t.Errorf("humanBytes(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// A run whose process died reads as "running" forever without this.
func TestRunMarkSeparatesADeadRunFromALiveOne(t *testing.T) {
	live := runItem{run: store.Run{Status: "running"}}
	dead := runItem{run: store.Run{Status: "running", Findings: 1}, orphaned: true}
	if got := runMark(live); got != "." {
		t.Errorf("live run mark = %q, want .", got)
	}
	if got := runMark(dead); got != "?" {
		t.Errorf("dead run mark = %q, want ? rather than the running dot", got)
	}
}
