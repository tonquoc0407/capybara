package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/tonquoc0407/capybara/internal/store"
	"github.com/tonquoc0407/capybara/internal/theme"
)

// glamour styles words individually, so escape codes split plain substrings.
var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func plainView(m detailModel) string {
	return ansiRE.ReplaceAllString(m.view(), "")
}

func testDetail() detailModel {
	m := newDetail(theme.Bara())
	m.setSize(60, 20)
	sp := span("llm1", "root", store.KindLLM, "chat", 1, 2)
	sp.Attrs = store.Attrs{Model: "fake-gpt", Raw: map[string]any{"custom.key": "custom-value"}}
	m.setSpan(sp, []store.Content{
		{SpanID: "llm1", Role: "user", Seq: 0, Body: "plain question", MediaType: "text/plain"},
		{SpanID: "llm1", Role: "output", Seq: 1, Body: `{"price":42}`, MediaType: "application/json"},
	}, nil)
	return m
}

func TestDetailRendersContents(t *testing.T) {
	m := testDetail()
	out := plainView(m)
	for _, want := range []string{"chat", "fake-gpt", "user", "plain question", "output", `"price": 42`} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q", want)
		}
	}
}

func TestDetailRawAttrsToggle(t *testing.T) {
	m := testDetail()
	m.update(press("a"))
	out := plainView(m)
	if !strings.Contains(out, "custom.key") || !strings.Contains(out, "custom-value") {
		t.Errorf("raw attrs view missing raw keys:\n%s", out)
	}
	if strings.Contains(out, "plain question") {
		t.Error("raw attrs view still shows contents")
	}
	m.update(press("a"))
	if !strings.Contains(plainView(m), "plain question") {
		t.Error("contents not restored after second toggle")
	}
}

func TestDetailWithoutContent(t *testing.T) {
	m := newDetail(theme.Bara())
	m.setSize(60, 20)
	m.setSpan(span("t1", "", store.KindTool, "probe", 0, 1), nil, nil)
	if !strings.Contains(plainView(m), "no content recorded") {
		t.Error("missing no-content notice")
	}
}
