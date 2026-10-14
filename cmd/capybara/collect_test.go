package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"

	"github.com/tonquoc0407/capybara/internal/store"
)

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func TestCollectReceivesAnalyzesAndStops(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "headless.db")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out syncBuffer
	done := make(chan error, 1)
	go func() {
		done <- collectCmd(ctx, db, "127.0.0.1:0", true,
			[]string{"-grpc", "127.0.0.1:0"}, &out)
	}()

	base := waitForStatusLine(t, &out, "otlp http ")
	postToolError(t, base+"/v1/traces")
	waitForFinding(t, db, "tool_error")
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("collect shutdown: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("collect did not stop after cancellation")
	}
	status := out.String()
	for _, want := range []string{"database " + db, "otlp grpc 127.0.0.1:", "content recorded"} {
		if !strings.Contains(status, want) {
			t.Errorf("status missing %q:\n%s", want, status)
		}
	}
}

func TestCollectContinuesWhenHTTPPortIsBusy(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out strings.Builder
	err = collectCmd(ctx, filepath.Join(t.TempDir(), "test.db"), held.Addr().String(), false,
		[]string{"-grpc", "127.0.0.1:0"}, &out)
	if err != nil {
		t.Fatalf("collect with one transport: %v", err)
	}
	for _, want := range []string{"otlp http unavailable", "otlp grpc 127.0.0.1:", "content dropped", "warning otlp http listen"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("status missing %q:\n%s", want, out.String())
		}
	}
}

func TestCollectRejectsArgumentsAndInvalidClaudeRoot(t *testing.T) {
	db := filepath.Join(t.TempDir(), "test.db")
	if err := collectCmd(context.Background(), db, "127.0.0.1:0", true,
		[]string{"extra"}, io.Discard); err == nil || !strings.Contains(err.Error(), "usage: capybara") {
		t.Fatalf("extra argument = %v, want usage error", err)
	}
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := collectCmd(context.Background(), db, "127.0.0.1:0", true,
		[]string{"-claude-root", file}, io.Discard); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("file claude root = %v, want directory error", err)
	}
}

func waitForStatusLine(t *testing.T, out *syncBuffer, prefix string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, line := range strings.Split(out.String(), "\n") {
			if value, ok := strings.CutPrefix(line, prefix); ok {
				return value
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("status never contained %q:\n%s", prefix, out.String())
	return ""
}

func postToolError(t *testing.T, endpoint string) {
	t.Helper()
	req := ptraceotlp.NewExportRequest()
	span := req.Traces().ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetTraceID(pcommon.TraceID{1})
	span.SetSpanID(pcommon.SpanID{2})
	span.SetName("execute_tool lookup")
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(time.Millisecond)))
	span.Attributes().PutStr("gen_ai.operation.name", "execute_tool")
	span.Attributes().PutStr("gen_ai.tool.name", "lookup")
	span.Attributes().PutStr("gen_ai.tool.call.result", `{"error":"boom"}`)
	raw, err := req.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(endpoint, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("post OTLP: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post OTLP status = %d", resp.StatusCode)
	}
}

func waitForFinding(t *testing.T, dbPath, findingType string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		st, err := store.Open(dbPath)
		if err == nil {
			findings, queryErr := st.AllFindings(context.Background())
			_ = st.Close()
			if queryErr == nil {
				for _, finding := range findings {
					if finding.Type == findingType {
						return
					}
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("finding %q did not appear", findingType)
}
