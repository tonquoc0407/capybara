package hub

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"

	"github.com/tonquoc0407/capybara/internal/store"
)

func TestHubTokenRolesIsolationAndLateLogs(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	admin, adminSecret, err := st.CreateHubToken(ctx, "alpha", "admin")
	if err != nil {
		t.Fatal(err)
	}
	_, viewerSecret, err := st.CreateHubToken(ctx, "alpha", "viewer")
	if err != nil {
		t.Fatal(err)
	}
	_, betaSecret, err := st.CreateHubToken(ctx, "beta", "editor")
	if err != nil {
		t.Fatal(err)
	}
	h := (Server{Store: st}).Handler() // hub defaults to no content
	call := func(method, path, secret, contentType string, body []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		if secret != "" {
			r.Header.Set("Authorization", "Bearer "+secret)
		}
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if got := call("GET", "/api/runs", "bad", "", nil); got.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token: %d", got.Code)
	}
	if got := call("GET", "/api/runs", viewerSecret, "", nil); got.Code != http.StatusOK {
		t.Fatalf("viewer read: %d", got.Code)
	}

	logReq := plogotlp.NewExportRequest()
	l := logReq.Logs().ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	l.SetTraceID(pcommon.TraceID{1})
	l.SetSpanID(pcommon.SpanID{2})
	l.Body().SetStr("private log")
	logRaw, err := logReq.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if got := call("POST", "/v1/logs", viewerSecret, "application/json", logRaw); got.Code != http.StatusForbidden {
		t.Fatalf("viewer ingest: %d", got.Code)
	}
	for range 2 {
		if got := call("POST", "/v1/logs", adminSecret, "application/json", logRaw); got.Code != http.StatusOK {
			t.Fatalf("late log: %d %s", got.Code, got.Body.String())
		}
	}
	traceReq := ptraceotlp.NewExportRequest()
	rs := traceReq.Traces().ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("capybara.project", "beta") // ignored in hub
	sp := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	sp.SetTraceID(pcommon.TraceID{1})
	sp.SetSpanID(pcommon.SpanID{2})
	sp.SetName("agent")
	sp.Attributes().PutStr("gen_ai.prompt", "private prompt")
	traceRaw, err := traceReq.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if got := call("POST", "/v1/traces", adminSecret, "application/json", traceRaw); got.Code != http.StatusOK {
		t.Fatalf("trace: %d %s", got.Code, got.Body.String())
	}
	runID := sp.TraceID().String()
	if !st.RunBelongsToProject(ctx, runID, "alpha") {
		t.Fatal("resource attribute overrode authenticated project")
	}
	if got := call("GET", "/api/v1/runs/"+runID, betaSecret, "", nil); got.Code != http.StatusNotFound {
		t.Fatalf("cross-project read: %d", got.Code)
	}
	if got := call("POST", "/v1/traces", betaSecret, "application/json", traceRaw); got.Code != http.StatusConflict {
		t.Fatalf("cross-project overwrite: %d", got.Code)
	}
	otherTrace := ptraceotlp.NewExportRequest()
	otherSpan := otherTrace.Traces().ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	otherSpan.SetTraceID(pcommon.TraceID{3})
	otherSpan.SetSpanID(pcommon.SpanID{2}) // existing span under alpha
	otherSpan.SetName("collision")
	otherRaw, err := otherTrace.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if got := call("POST", "/v1/traces", betaSecret, "application/json", otherRaw); got.Code != http.StatusConflict {
		t.Fatalf("span id collision: %d", got.Code)
	}
	if st.RunBelongsToProject(ctx, otherSpan.TraceID().String(), "beta") {
		t.Fatal("failed batch left a run behind")
	}
	if got := call("GET", "/api/v1/runs/"+runID+"/logs", adminSecret, "", nil); got.Code != http.StatusOK || strings.Contains(got.Body.String(), "private log") {
		t.Fatalf("redacted log: %d %s", got.Code, got.Body.String())
	}
	logs, err := st.TraceLogs(ctx, runID)
	if err != nil || len(logs) != 1 {
		t.Fatalf("duplicate log: %+v %v", logs, err)
	}
	if got := call("GET", "/api/v1/audit", viewerSecret, "", nil); got.Code != http.StatusForbidden {
		t.Fatalf("viewer audit: %d", got.Code)
	}
	if got := call("GET", "/api/v1/audit", adminSecret, "", nil); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), "token.create") {
		t.Fatalf("admin audit: %d %s", got.Code, got.Body.String())
	}
	basic := httptest.NewRequest("GET", "/api/runs", nil)
	basic.SetBasicAuth("user", viewerSecret)
	basicResponse := httptest.NewRecorder()
	h.ServeHTTP(basicResponse, basic)
	if basicResponse.Code != http.StatusOK {
		t.Fatalf("basic auth: %d", basicResponse.Code)
	}
	if err := st.RevokeHubToken(ctx, admin.ID); err != nil {
		t.Fatal(err)
	}
	if got := call("GET", "/api/runs", adminSecret, "", nil); got.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token: %d", got.Code)
	}
}

func TestHubRejectsPublicHTTPBind(t *testing.T) {
	if err := (Server{}).Serve(context.Background(), "0.0.0.0:7655", &bytes.Buffer{}); err == nil {
		t.Fatal("accepted public plaintext bind")
	}
}

func TestHubRedactsCapturedTraceAndLogOverHTTP(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "redact.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_, secret, err := st.CreateHubToken(ctx, "alpha", "editor")
	if err != nil {
		t.Fatal(err)
	}
	h := (Server{Store: st, CaptureContent: true, RedactPattern: regexp.MustCompile(`secret-[0-9]+`)}).Handler()
	call := func(path string, raw []byte) {
		r := httptest.NewRequest("POST", path, bytes.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+secret)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	traces := ptraceotlp.NewExportRequest()
	sp := traces.Traces().ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	sp.SetTraceID(pcommon.TraceID{4})
	sp.SetSpanID(pcommon.SpanID{5})
	sp.SetName("call secret-42")
	sp.Attributes().PutStr("gen_ai.prompt", "secret-42")
	raw, _ := traces.MarshalJSON()
	call("/v1/traces", raw)
	logsReq := plogotlp.NewExportRequest()
	log := logsReq.Logs().ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	log.SetTraceID(sp.TraceID())
	log.SetSpanID(sp.SpanID())
	log.Body().SetStr("secret-42")
	log.Attributes().PutStr("token", "secret-42")
	raw, _ = logsReq.MarshalJSON()
	call("/v1/logs", raw)
	spans, err := st.Spans(ctx, sp.TraceID().String())
	if err != nil || len(spans) != 1 || strings.Contains(spans[0].Name, "secret-42") {
		t.Fatalf("span leaked: %+v, %v", spans, err)
	}
	contents, err := st.Contents(ctx, sp.SpanID().String())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range contents {
		if strings.Contains(c.Body, "secret-42") {
			t.Fatalf("content leaked: %+v", c)
		}
	}
	logs, err := st.TraceLogs(ctx, sp.TraceID().String())
	if err != nil || len(logs) != 1 || strings.Contains(logs[0].Body+logs[0].AttrsJSON, "secret-42") {
		t.Fatalf("log leaked: %+v, %v", logs, err)
	}
}

func TestHubRetentionPrunesOldUnpinnedRuns(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "retention.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	old := time.Now().UTC().Add(-72 * time.Hour)
	sp := store.Span{ID: "old-span", RunID: "old-run", Kind: store.KindAgent,
		Name: "old", StartedAt: old, EndedAt: old.Add(time.Second), Status: "ok"}
	if err := st.WriteBatch(ctx, store.Batch{Source: "test", Spans: []store.Span{sp}}); err != nil {
		t.Fatal(err)
	}
	if err := (Server{Store: st, Retention: 24 * time.Hour}).applyRetention(ctx); err != nil {
		t.Fatal(err)
	}
	if st.RunBelongsToProject(ctx, "old-run", "default") {
		t.Fatal("old run survived retention")
	}
	events, err := st.AuditEvents(ctx, "default", 10)
	if err != nil || len(events) != 1 || events[0].Action != "retention.prune" {
		t.Fatalf("audit: %+v %v", events, err)
	}
}
