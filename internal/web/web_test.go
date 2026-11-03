package web

import (
	"bufio"
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

func TestV1RunsPaginateAndLoadContentOnDemand(t *testing.T) {
	st := seeded(t)
	if err := st.PutTraceLogs(context.Background(), "otlp", []store.TraceLog{{
		ID: "log-1", RunID: "run-one", SpanID: "t1", Body: "private-log", AttrsJSON: `{}`,
	}}); err != nil {
		t.Fatal(err)
	}
	h := Handler(st)
	request := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, path, nil))
		return r
	}
	page := request("/api/v1/runs?project=default&limit=1")
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `"id":"run-one"`) {
		t.Fatalf("page = %d %s", page.Code, page.Body.String())
	}
	if got := request("/api/v1/runs?limit=101"); got.Code != http.StatusBadRequest {
		t.Fatalf("invalid limit = %d", got.Code)
	}
	detail := request("/api/v1/runs/run-one")
	if detail.Code != http.StatusOK || strings.Contains(detail.Body.String(), `price`) || strings.Contains(detail.Body.String(), `private-log`) {
		t.Fatalf("eager content = %d %s", detail.Code, detail.Body.String())
	}
	logs := request("/api/v1/runs/run-one/logs")
	if logs.Code != http.StatusOK || !strings.Contains(logs.Body.String(), `private-log`) {
		t.Fatalf("logs = %d %s", logs.Code, logs.Body.String())
	}
	content := request("/api/v1/runs/run-one/spans/t1/contents")
	if content.Code != http.StatusOK || !strings.Contains(content.Body.String(), `price`) {
		t.Fatalf("content = %d %s", content.Code, content.Body.String())
	}
	var contentRows []map[string]any
	if err := json.Unmarshal(content.Body.Bytes(), &contentRows); err != nil || len(contentRows) != 1 || contentRows[0]["role"] != "output" || contentRows[0]["body"] != `{"price":42}` {
		t.Fatalf("lazy content differs from RunDetail's JSON shape: %+v, %v", contentRows, err)
	}
	if got := request("/api/v1/runs/run-one/spans/other/contents"); got.Code != http.StatusNotFound {
		t.Fatalf("foreign span = %d", got.Code)
	}
}

func TestV1RunSearchFiltersBeforePagingAndRejectsBadTime(t *testing.T) {
	st := seeded(t)
	h := Handler(st)
	request := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	if got := request("/api/v1/runs?q=run-one&status=ok&from=2026-07-22T09%3A00%3A00Z"); got.Code != 200 || !strings.Contains(got.Body.String(), "run-one") {
		t.Fatalf("search missed run: %d %s", got.Code, got.Body.String())
	}
	if got := request("/api/v1/runs?q=run-one&status=error"); got.Code != 200 || strings.Contains(got.Body.String(), `"id":"run-one"`) {
		t.Fatalf("status filter failed: %d %s", got.Code, got.Body.String())
	}
	if got := request("/api/v1/runs?from=yesterday"); got.Code != 400 {
		t.Fatalf("invalid time accepted: %d", got.Code)
	}
}

func TestOperationsAndIncidentsShareTimeAndReleaseFilters(t *testing.T) {
	h := Handler(seeded(t))
	request := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	query := "?project=default&from=2026-07-22T09:00:00Z&to=2026-07-22T11:00:00Z"
	w := request("/api/v1/operations" + query)
	var report store.OperationsReport
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatalf("%d: %s: %v", w.Code, w.Body.String(), err)
	}
	if report.Total.Runs != 1 || report.Total.P95LatencyMS == nil || *report.Total.P95LatencyMS != 10000 {
		t.Fatalf("report: %+v", report)
	}
	for _, path := range []string{"/api/v1/operations", "/api/v1/incidents"} {
		w := request(path + query + "&release=missing")
		if w.Code != 200 || strings.Contains(w.Body.String(), "run-one") {
			t.Fatalf("release filter %s: %d %s", path, w.Code, w.Body.String())
		}
		if path == "/api/v1/operations" {
			if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil || report.Total.Runs != 0 {
				t.Fatalf("release totals: %+v, %v", report, err)
			}
		}
		if w := request(path + "?from=invalid"); w.Code != 400 {
			t.Fatalf("invalid time %s: %d", path, w.Code)
		}
	}
}

func TestIncidentPacketHTMLRouteContainsEvidence(t *testing.T) {
	h := Handler(seeded(t))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/runs/run-one/incident-packet.html", nil))
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "text/html") ||
		!strings.Contains(w.Body.String(), "Detector hypotheses") || !strings.Contains(w.Body.String(), "fetch") {
		t.Fatalf("HTML packet: %d %s", w.Code, w.Body.String())
	}
}

func TestV1QualityReadEndpoints(t *testing.T) {
	st := seeded(t)
	d, err := st.CreateDataset(context.Background(), "default", "regressions")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddScore(context.Background(), "run-one", "", "quality", 0.8, "human", "reviewed"); err != nil {
		t.Fatal(err)
	}
	h := Handler(st)
	request := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, path, nil))
		return r
	}
	if got := request("/api/v1/datasets?project=default"); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), d.ID) {
		t.Fatalf("datasets: %d %s", got.Code, got.Body.String())
	}
	if got := request("/api/v1/runs/run-one/scores"); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"value":0.8`) {
		t.Fatalf("scores: %d %s", got.Code, got.Body.String())
	}
	if got := request("/api/v1/experiments/compare"); got.Code != http.StatusBadRequest {
		t.Fatalf("incomplete compare: %d", got.Code)
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

func TestHandlerEventsSSE(t *testing.T) {
	st := seeded(t)
	srv := httptest.NewServer(Handler(st))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/events", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/events: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	r := bufio.NewReader(resp.Body)
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read initial comment: %v", err)
	}
	if !strings.HasPrefix(line, ": connected") {
		t.Errorf("initial line = %q, want : connected", line)
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = st.WriteBatch(context.Background(), store.Batch{
			Source: "test",
			Spans: []store.Span{{
				ID: "live-span", RunID: "run-live", Kind: store.KindTool, Name: "live_call",
				StartedAt: t0, EndedAt: t0.Add(time.Second), Status: "ok",
			}},
		})
	}()

	received := false
	for {
		l, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read sse event: %v", err)
		}
		if strings.HasPrefix(l, "event: update") {
			received = true
			break
		}
	}
	if !received {
		t.Error("did not receive event: update")
	}
}
