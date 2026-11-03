package hub

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
)

type announcement chan string

func (a announcement) Write(p []byte) (int, error) {
	if strings.HasPrefix(string(p), "http://") {
		a <- strings.TrimSpace(string(p))
	}
	return len(p), nil
}

func TestLiveHubAnalyzesExistingAndIncomingTracesAndStopsStreams(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st, err := store.Open(filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	_, token, err := st.CreateHubToken(ctx, "alpha", "admin")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := st.WriteBatch(ctx, store.Batch{Source: "test", HubProject: "alpha", Spans: []store.Span{{
		ID: "startup-span", RunID: "startup", Kind: store.KindLLM, Name: "chat", Status: "ok",
		StartedAt: now.Add(-time.Minute), EndedAt: now.Add(-time.Minute + time.Second),
		TokensIn: 100, TokensOut: 20, Attrs: store.Attrs{Model: "gpt-4o-mini"},
	}}}); err != nil {
		t.Fatal(err)
	}
	addr := make(announcement, 1)
	done := make(chan error, 1)
	go func() { done <- (Server{Store: st, CaptureContent: true}).Serve(ctx, "127.0.0.1:0", addr) }()
	var base string
	select {
	case base = <-addr:
	case err := <-done:
		t.Fatalf("serve: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("hub did not announce")
	}
	stopped := false
	t.Cleanup(func() {
		cancel()
		if !stopped {
			select {
			case <-done:
			case <-time.After(6 * time.Second):
				t.Error("hub did not stop")
			}
		}
	})
	client := &http.Client{Timeout: 3 * time.Second}
	call := func(method, path string, body []byte) *http.Response {
		t.Helper()
		r, err := http.NewRequest(method, base+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer "+token)
		if body != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != 200 {
			raw, _ := io.ReadAll(response.Body)
			response.Body.Close()
			select {
			case serverErr := <-done:
				stopped = true
				t.Fatalf("%s: %d %s; server: %v", path, response.StatusCode, raw, serverErr)
			case <-time.After(100 * time.Millisecond):
				t.Fatalf("%s: %d %s; server still running", path, response.StatusCode, raw)
			}
		}
		return response
	}
	export := ptraceotlp.NewExportRequest()
	spans := export.Traces().ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans()
	tool := spans.AppendEmpty()
	tool.SetTraceID(pcommon.TraceID{1})
	tool.SetSpanID(pcommon.SpanID{1})
	tool.SetName("lookup")
	tool.SetStartTimestamp(pcommon.NewTimestampFromTime(now))
	tool.SetEndTimestamp(pcommon.NewTimestampFromTime(now.Add(time.Second)))
	tool.Attributes().PutStr("gen_ai.operation.name", "execute_tool")
	tool.Attributes().PutStr("gen_ai.tool.name", "lookup")
	tool.Attributes().PutStr("gen_ai.tool.call.result", `{"error":"upstream unavailable"}`)
	llm := spans.AppendEmpty()
	llm.SetTraceID(pcommon.TraceID{2})
	llm.SetSpanID(pcommon.SpanID{2})
	llm.SetName("chat")
	llm.SetStartTimestamp(pcommon.NewTimestampFromTime(now))
	llm.SetEndTimestamp(pcommon.NewTimestampFromTime(now.Add(time.Second)))
	llm.Attributes().PutStr("gen_ai.operation.name", "chat")
	llm.Attributes().PutStr("gen_ai.request.model", "gpt-4o-mini")
	llm.Attributes().PutInt("gen_ai.usage.input_tokens", 100)
	llm.Attributes().PutInt("gen_ai.usage.output_tokens", 20)
	raw, err := export.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	call("POST", "/v1/traces", raw).Body.Close()
	ready := false
	for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
		old, e1 := st.GetRun(ctx, "startup")
		live, e2 := st.GetRun(ctx, llm.TraceID().String())
		findings, e3 := st.Findings(ctx, tool.TraceID().String())
		if e1 == nil && e2 == nil && e3 == nil && old.CostUSD != nil && *old.CostUSD > 0 && live.CostUSD != nil && *live.CostUSD > 0 {
			for _, f := range findings {
				if f.Type == "tool_error" {
					ready = true
				}
			}
		}
		if ready {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatal("hub did not analyze startup and incoming traces")
	}
	stream := call("GET", "/api/events", nil)
	defer stream.Body.Close()
	cancel()
	select {
	case err := <-done:
		stopped = true
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("active SSE stream delayed hub shutdown")
	}
}
