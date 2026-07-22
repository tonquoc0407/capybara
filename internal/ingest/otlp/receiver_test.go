package otlp

import (
	"bytes"
	"compress/gzip"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	gzipenc "google.golang.org/grpc/encoding/gzip"

	"github.com/tonquoc0407/capybara/internal/store"
)

func openTemp(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func chatRequest(t *testing.T) ptraceotlp.ExportRequest {
	t.Helper()
	td, span := singleSpan()
	span.SetName("chat fake-gpt")
	span.Attributes().PutStr("gen_ai.operation.name", "chat")
	span.Attributes().PutStr("gen_ai.request.model", "fake-gpt")
	span.Attributes().PutInt("gen_ai.usage.input_tokens", 5)
	return ptraceotlp.NewExportRequestFromTraces(td)
}

func wantOneChatSpan(t *testing.T, st *store.Store) {
	t.Helper()
	runs, err := st.ListRuns(context.Background())
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].Source != "otlp" || runs[0].TokensIn != 5 {
		t.Fatalf("runs = %+v", runs)
	}
	spans, err := st.Spans(context.Background(), runs[0].ID)
	if err != nil {
		t.Fatalf("Spans: %v", err)
	}
	if len(spans) != 1 || spans[0].Kind != store.KindLLM || spans[0].Attrs.Model != "fake-gpt" {
		t.Fatalf("spans = %+v", spans)
	}
}

// startGRPC serves a receiver on an ephemeral port and returns a client for it.
func startGRPC(t *testing.T, st *store.Store) ptraceotlp.GRPCClient {
	t.Helper()
	r := New(st, true)
	grpcLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	httpLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.serve(ctx, grpcLis, httpLis) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("serve: %v", err)
		}
	})
	conn, err := grpc.NewClient(grpcLis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc client: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return ptraceotlp.NewGRPCClient(conn)
}

func TestGRPCExport(t *testing.T) {
	st := openTemp(t)
	client := startGRPC(t, st)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.Export(ctx, chatRequest(t)); err != nil {
		t.Fatalf("Export: %v", err)
	}
	wantOneChatSpan(t, st)
}

func TestGRPCExportGzipped(t *testing.T) {
	st := openTemp(t)
	client := startGRPC(t, st)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.Export(ctx, chatRequest(t), grpc.UseCompressor(gzipenc.Name)); err != nil {
		t.Fatalf("gzipped Export: %v", err)
	}
	wantOneChatSpan(t, st)
}

func TestGRPCExportAcceptsBatchOverFourMiB(t *testing.T) {
	st := openTemp(t)
	client := startGRPC(t, st)
	td, span := singleSpan()
	span.Attributes().PutStr("gen_ai.operation.name", "chat")
	span.Attributes().PutStr("gen_ai.input.messages", strings.Repeat("x", 5<<20))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := client.Export(ctx, ptraceotlp.NewExportRequestFromTraces(td)); err != nil {
		t.Fatalf("large Export: %v", err)
	}
	contents, err := st.Contents(context.Background(), "0102030405060708")
	if err != nil {
		t.Fatalf("Contents: %v", err)
	}
	if len(contents) != 1 || len(contents[0].Body) != 5<<20 {
		t.Fatalf("large content not stored: %d rows", len(contents))
	}
}

func TestHTTPExportJSON(t *testing.T) {
	st := openTemp(t)
	srv := httptest.NewServer(New(st, true).handler())
	defer srv.Close()
	body, err := chatRequest(t).MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := http.Post(srv.URL+"/v1/traces", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	wantOneChatSpan(t, st)
}

func TestHTTPExportGzippedProto(t *testing.T) {
	st := openTemp(t)
	srv := httptest.NewServer(New(st, true).handler())
	defer srv.Close()
	raw, err := chatRequest(t).MarshalProto()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(raw); err != nil {
		t.Fatalf("gzip: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/traces", &buf)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Content-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	wantOneChatSpan(t, st)
}

func TestHTTPRejectsUnknownContentType(t *testing.T) {
	st := openTemp(t)
	srv := httptest.NewServer(New(st, true).handler())
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/v1/traces", "text/csv", bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", resp.StatusCode)
	}
}
