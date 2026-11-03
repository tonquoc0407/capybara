package proxy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tonquoc0407/capybara/internal/store"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func makeRequest(h http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestProxyRecordsEstimatedCostAndBlocksLaterBudgetCall(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	calls := 0
	s := &Server{
		Store: st, UpstreamURL: "https://example.test/v1/chat/completions", APIKey: "upstream-secret",
		MaxCostUSD: 4, InputPerM: 1000000, OutputPerM: 1000000,
		Client: &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			if req.Header.Get("Authorization") != "Bearer upstream-secret" {
				t.Error("API key was not forwarded")
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[],"usage":{"prompt_tokens":2,"completion_tokens":3}}`))}, nil
		})},
	}
	body := `{"model":"test-model","messages":[{"role":"user","content":"hello"}]}`
	first := makeRequest(s.Handler(), body)
	if first.Code != 200 || first.Header().Get("X-Capybara-Trace-ID") == "" {
		t.Fatalf("first response: %d %s", first.Code, first.Body.String())
	}
	second := makeRequest(s.Handler(), body)
	if second.Code != http.StatusPaymentRequired || calls != 1 {
		t.Fatalf("budget did not stop second call: %d, upstream calls %d", second.Code, calls)
	}
	runs, err := st.ListRuns(context.Background())
	if err != nil || len(runs) != 1 || runs[0].CostUSD == nil || *runs[0].CostUSD != 5 || runs[0].TokensIn != 2 || runs[0].TokensOut != 3 {
		t.Fatalf("recorded run: %+v, %v", runs, err)
	}
	spans, _ := st.Spans(context.Background(), runs[0].ID)
	contents, _ := st.Contents(context.Background(), spans[0].ID)
	if len(contents) != 0 {
		t.Fatal("proxy stored content without opt-in")
	}
	provenance, err := st.CostProvenanceForRun(context.Background(), runs[0].ID)
	if err != nil || len(provenance) != 1 || provenance[0].Kind != "estimate" {
		t.Fatalf("provenance: %+v, %v", provenance, err)
	}
}

func TestProxyGuardrailAndUnknownUsage(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	calls := 0
	s := &Server{
		Store: st, UpstreamURL: "https://example.test/v1/chat/completions",
		DenyPattern: regexp.MustCompile("blocked"), MaxCostUSD: 1, InputPerM: 1, OutputPerM: 1,
		Client: &http.Client{Transport: transportFunc(func(_ *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[]}`))}, nil
		})},
	}
	if got := makeRequest(s.Handler(), `{"model":"m","messages":[{"content":"blocked"}]}`); got.Code != http.StatusUnprocessableEntity || calls != 0 {
		t.Fatalf("request guardrail: %d calls %d", got.Code, calls)
	}
	body := `{"model":"m","messages":[{"content":"safe"}]}`
	if got := makeRequest(s.Handler(), body); got.Code != 200 {
		t.Fatalf("unknown usage first call: %d", got.Code)
	}
	if got := makeRequest(s.Handler(), body); got.Code != http.StatusPaymentRequired || calls != 1 {
		t.Fatalf("unknown usage did not close budget: %d calls %d", got.Code, calls)
	}
}

func TestProxyEmptyUsageCannotBypassBudget(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	calls := 0
	s := &Server{
		Store: st, UpstreamURL: "https://example.test/v1/chat/completions",
		MaxCostUSD: 1, InputPerM: 1, OutputPerM: 1,
		Client: &http.Client{Transport: transportFunc(func(_ *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{
				StatusCode: 200, Header: make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{"choices":[],"usage":{}}`)),
			}, nil
		})},
	}
	body := `{"model":"m","messages":[{"content":"safe"}]}`
	if got := makeRequest(s.Handler(), body); got.Code != http.StatusOK {
		t.Fatalf("first response: %d", got.Code)
	}
	if got := makeRequest(s.Handler(), body); got.Code != http.StatusPaymentRequired || calls != 1 {
		t.Fatalf("empty usage bypassed budget: %d calls %d", got.Code, calls)
	}
	runs, err := st.ListRuns(context.Background())
	if err != nil || len(runs) != 1 || runs[0].CostUSD != nil {
		t.Fatalf("empty usage produced a cost: %+v, %v", runs, err)
	}
}
