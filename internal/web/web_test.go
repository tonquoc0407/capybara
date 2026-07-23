package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

var t0 = time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)

func seeded(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	b := store.Batch{
		Source: "otlp",
		Spans: []store.Span{
			{
				ID: "root", RunID: "run-one", Kind: store.KindAgent, Name: "agent_loop",
				StartedAt: t0, EndedAt: t0.Add(10 * time.Second), Status: "ok",
			},
			{
				ID: "t1", RunID: "run-one", ParentID: "root", Kind: store.KindTool,
				Name: "fetch", StartedAt: t0.Add(time.Second),
				EndedAt: t0.Add(2 * time.Second), Status: "error",
				Attrs: store.Attrs{ToolName: "fetch"},
			},
		},
		Contents: []store.Content{
			{SpanID: "t1", Role: "output", Seq: 0, Body: `{"price":42}`, MediaType: "application/json"},
		},
		Findings: []store.Finding{
			{
				RunID: "run-one", SpanID: "t1", Type: "drift", Severity: "warning",
				Detail: `{"missing":["currency"]}`,
			},
		},
	}
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	return st
}

func TestRunCarriesSpansFindingsAndContent(t *testing.T) {
	st := seeded(t)
	detail, err := Run(context.Background(), st, "run-one")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(detail.Spans) != 2 {
		t.Fatalf("spans = %d, want 2", len(detail.Spans))
	}
	tool := detail.Spans[1]
	if tool.Parent != "root" || tool.Tool != "fetch" || tool.Status != "error" {
		t.Errorf("tool span = %+v", tool)
	}
	if tool.Duration != 1 {
		t.Errorf("duration = %v, want 1", tool.Duration)
	}
	if len(detail.Findings) != 1 || detail.Findings[0].Summary != "missing field: currency" {
		t.Fatalf("findings = %+v", detail.Findings)
	}
	if got := detail.Contents["t1"]; len(got) != 1 || got[0].Role != "output" {
		t.Errorf("contents = %+v", detail.Contents)
	}
}

func TestPageInlinesItsOwnAssets(t *testing.T) {
	body, err := Page(nil)
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	page := string(body)
	for _, want := range []string{"window.CAPYBARA = null", "--accent:", "capybara"} {
		if !strings.Contains(page, want) {
			t.Errorf("page missing %q", want)
		}
	}
	for _, forbidden := range []string{"<script src", "<link rel=\"stylesheet\"", "@import"} {
		if strings.Contains(page, forbidden) {
			t.Errorf("page loads something external: %q", forbidden)
		}
	}
}

func TestWriteHTMLIsSelfContained(t *testing.T) {
	st := seeded(t)
	dir := t.TempDir()
	path, err := WriteHTML(context.Background(), st, "run-one", dir)
	if err != nil {
		t.Fatalf("WriteHTML: %v", err)
	}
	if filepath.Base(path) != "run-one.html" {
		t.Errorf("path = %s", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read page: %v", err)
	}
	page := string(raw)
	if strings.Contains(page, "window.CAPYBARA = null") {
		t.Error("exported page has no data inlined")
	}
	for _, want := range []string{"agent_loop", "missing field: currency", `price`} {
		if !strings.Contains(page, want) {
			t.Errorf("exported page missing %q", want)
		}
	}
	if strings.Contains(page, "<script src") {
		t.Error("exported page is not self-contained")
	}
}

func TestHandlerServesPageAndData(t *testing.T) {
	srv := httptest.NewServer(Handler(seeded(t)))
	defer srv.Close()

	body := getBody(t, srv.URL+"/")
	if !strings.Contains(body, "<title>capybara</title>") {
		t.Errorf("index = %.120s", body)
	}
	var runs []RunSummary
	decode(t, srv.URL+"/api/runs", &runs)
	if len(runs) != 1 || runs[0].ID != "run-one" || runs[0].Findings != 1 {
		t.Fatalf("runs = %+v", runs)
	}
	// The id in the path is resolved by prefix, like every other command.
	var detail RunDetail
	decode(t, srv.URL+"/api/runs/run-o", &detail)
	if detail.ID != "run-one" || len(detail.Spans) != 2 {
		t.Errorf("detail = %+v", detail)
	}
	resp, err := http.Get(srv.URL + "/api/runs/nope")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown run = %d, want 404", resp.StatusCode)
	}
}

func getBody(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get %s = %d", url, resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return string(raw)
}

func decode(t *testing.T, url string, into any) {
	t.Helper()
	if err := json.Unmarshal([]byte(getBody(t, url)), into); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
}
