package mcp

import (
	"context"
	"fmt"
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

func openTemp(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestProxyStreamsToolsCallSuccess(t *testing.T) {
	st := openTemp(t)
	runID := "mcp-test-1"
	proxy := NewStreamProxy(st, runID, "root-1", true)

	clientInR, clientInW := io.Pipe()
	clientOutR, clientOutW := io.Pipe()
	serverInR, serverInW := io.Pipe()
	serverOutR, serverOutW := io.Pipe()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errc := make(chan error, 1)
	go func() {
		errc <- proxy.ProxyStreams(ctx, clientInR, clientOutW, serverInW, serverOutR)
	}()

	// Client sends tools/call request
	req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_weather","arguments":{"city":"Tokyo"}}}` + "\n"
	go func() {
		_, _ = clientInW.Write([]byte(req))
	}()

	// Server reads request from serverInR
	buf := make([]byte, 1024)
	n, err := serverInR.Read(buf)
	if err != nil {
		t.Fatalf("serverIn read: %v", err)
	}
	gotReq := string(buf[:n])
	if gotReq != req {
		t.Fatalf("server received %q, want %q", gotReq, req)
	}

	// Server sends response to serverOutW
	resp := `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Sunny 22C"}],"isError":false}}` + "\n"
	go func() {
		_, _ = serverOutW.Write([]byte(resp))
	}()

	// Client reads response from clientOutR
	n, err = clientOutR.Read(buf)
	if err != nil {
		t.Fatalf("clientOut read: %v", err)
	}
	gotResp := string(buf[:n])
	if gotResp != resp {
		t.Fatalf("client received %q, want %q", gotResp, resp)
	}

	// Close client input and server output to finish proxy streams
	_ = clientInW.Close()
	_ = serverOutW.Close()

	if err := <-errc; err != nil {
		t.Fatalf("ProxyStreams: %v", err)
	}

	// Verify spans recorded in store
	spans, err := st.Spans(context.Background(), runID)
	if err != nil {
		t.Fatalf("Spans: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	sp := spans[0]
	if sp.Kind != store.KindTool || sp.Name != "get_weather" || sp.Status != "ok" {
		t.Errorf("unexpected span: %+v", sp)
	}
	if !sp.Attrs.MCP || sp.Attrs.ToolName != "get_weather" {
		t.Errorf("unexpected attrs: %+v", sp.Attrs)
	}

	// Verify contents recorded
	contents, err := st.Contents(context.Background(), sp.ID)
	if err != nil {
		t.Fatalf("Contents: %v", err)
	}
	if len(contents) != 2 {
		t.Fatalf("got %d contents, want 2 (input and output)", len(contents))
	}
	if contents[0].Role != "input" || !strings.Contains(contents[0].Body, "Tokyo") {
		t.Errorf("input content unexpected: %+v", contents[0])
	}
	if contents[1].Role != "output" || !strings.Contains(contents[1].Body, "Sunny 22C") {
		t.Errorf("output content unexpected: %+v", contents[1])
	}
}

func TestProxyStreamsToolsCallErrorResponse(t *testing.T) {
	st := openTemp(t)
	runID := "mcp-test-err"
	proxy := NewStreamProxy(st, runID, "root-1", true)

	clientInR, clientInW := io.Pipe()
	clientOutR, clientOutW := io.Pipe()
	serverInR, serverInW := io.Pipe()
	serverOutR, serverOutW := io.Pipe()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errc := make(chan error, 1)
	go func() {
		errc <- proxy.ProxyStreams(ctx, clientInR, clientOutW, serverInW, serverOutR)
	}()

	req := `{"jsonrpc":"2.0","id":"err-1","method":"tools/call","params":{"name":"query_db","arguments":{"sql":"DROP TABLE"}}}` + "\n"
	go func() { _, _ = clientInW.Write([]byte(req)) }()

	buf := make([]byte, 1024)
	_, _ = serverInR.Read(buf)

	// Server responds with JSON-RPC error
	resp := `{"jsonrpc":"2.0","id":"err-1","error":{"code":-32600,"message":"Permission denied"}}` + "\n"
	go func() { _, _ = serverOutW.Write([]byte(resp)) }()

	_, _ = clientOutR.Read(buf)

	_ = clientInW.Close()
	_ = serverOutW.Close()

	if err := <-errc; err != nil {
		t.Fatalf("ProxyStreams: %v", err)
	}

	spans, err := st.Spans(context.Background(), runID)
	if err != nil {
		t.Fatalf("Spans: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	if spans[0].Status != "error" {
		t.Errorf("span status = %q, want error", spans[0].Status)
	}
}

func TestProxyStreamsToolsCallResultIsError(t *testing.T) {
	st := openTemp(t)
	runID := "mcp-test-iserr"
	proxy := NewStreamProxy(st, runID, "root-1", true)

	clientInR, clientInW := io.Pipe()
	clientOutR, clientOutW := io.Pipe()
	serverInR, serverInW := io.Pipe()
	serverOutR, serverOutW := io.Pipe()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errc := make(chan error, 1)
	go func() {
		errc <- proxy.ProxyStreams(ctx, clientInR, clientOutW, serverInW, serverOutR)
	}()

	req := `{"jsonrpc":"2.0","id":99,"method":"tools/call","params":{"name":"fetch","arguments":{"url":"http://down"}}}` + "\n"
	go func() { _, _ = clientInW.Write([]byte(req)) }()

	buf := make([]byte, 1024)
	_, _ = serverInR.Read(buf)

	// Server responds with result having isError: true (MCP convention)
	resp := `{"jsonrpc":"2.0","id":99,"result":{"content":[{"type":"text","text":"connection refused"}],"isError":true}}` + "\n"
	go func() { _, _ = serverOutW.Write([]byte(resp)) }()

	_, _ = clientOutR.Read(buf)

	_ = clientInW.Close()
	_ = serverOutW.Close()

	if err := <-errc; err != nil {
		t.Fatalf("ProxyStreams: %v", err)
	}

	spans, err := st.Spans(context.Background(), runID)
	if err != nil {
		t.Fatalf("Spans: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	if spans[0].Status != "error" {
		t.Errorf("span status = %q, want error for isError=true result", spans[0].Status)
	}
}

func TestProxyStreamsResourcesRead(t *testing.T) {
	st := openTemp(t)
	runID := "mcp-test-res"
	proxy := NewStreamProxy(st, runID, "root-1", true)

	clientInR, clientInW := io.Pipe()
	clientOutR, clientOutW := io.Pipe()
	serverInR, serverInW := io.Pipe()
	serverOutR, serverOutW := io.Pipe()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errc := make(chan error, 1)
	go func() {
		errc <- proxy.ProxyStreams(ctx, clientInR, clientOutW, serverInW, serverOutR)
	}()

	req := `{"jsonrpc":"2.0","id":5,"method":"resources/read","params":{"uri":"file:///schema.sql"}}` + "\n"
	go func() { _, _ = clientInW.Write([]byte(req)) }()

	buf := make([]byte, 1024)
	_, _ = serverInR.Read(buf)

	resp := `{"jsonrpc":"2.0","id":5,"result":{"contents":[{"uri":"file:///schema.sql","text":"CREATE TABLE t;"}]}}` + "\n"
	go func() { _, _ = serverOutW.Write([]byte(resp)) }()

	_, _ = clientOutR.Read(buf)

	_ = clientInW.Close()
	_ = serverOutW.Close()

	if err := <-errc; err != nil {
		t.Fatalf("ProxyStreams: %v", err)
	}

	spans, err := st.Spans(context.Background(), runID)
	if err != nil {
		t.Fatalf("Spans: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	if spans[0].Kind != store.KindRetrieval {
		t.Errorf("span kind = %q, want retrieval", spans[0].Kind)
	}
	if !strings.Contains(spans[0].Name, "schema.sql") {
		t.Errorf("span name = %q, want to contain schema.sql", spans[0].Name)
	}
}

func TestProxyStreamsSamplingCreateMessage(t *testing.T) {
	st := openTemp(t)
	runID := "mcp-test-sampling"
	proxy := NewStreamProxy(st, runID, "root-1", true)

	clientInR, clientInW := io.Pipe()
	clientOutR, clientOutW := io.Pipe()
	serverInR, serverInW := io.Pipe()
	serverOutR, serverOutW := io.Pipe()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errc := make(chan error, 1)
	go func() {
		errc <- proxy.ProxyStreams(ctx, clientInR, clientOutW, serverInW, serverOutR)
	}()

	// Server initiates sampling/createMessage request to client
	req := `{"jsonrpc":"2.0","id":77,"method":"sampling/createMessage","params":{"messages":[{"role":"user","content":{"type":"text","text":"hello"}}]}}` + "\n"
	go func() { _, _ = serverOutW.Write([]byte(req)) }()

	buf := make([]byte, 1024)
	_, _ = clientOutR.Read(buf)

	// Client sends response to server
	resp := `{"jsonrpc":"2.0","id":77,"result":{"role":"assistant","content":{"type":"text","text":"world"}}}` + "\n"
	go func() { _, _ = clientInW.Write([]byte(resp)) }()

	_, _ = serverInR.Read(buf)

	_ = clientInW.Close()
	_ = serverOutW.Close()

	if err := <-errc; err != nil {
		t.Fatalf("ProxyStreams: %v", err)
	}

	spans, err := st.Spans(context.Background(), runID)
	if err != nil {
		t.Fatalf("Spans: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	if spans[0].Kind != store.KindLLM {
		t.Errorf("span kind = %q, want llm", spans[0].Kind)
	}
}

func TestProxyStreamsNonJSON(t *testing.T) {
	proxy := NewStreamProxy(nil, "run-1", "root-1", false)

	clientInR, clientInW := io.Pipe()
	clientOutR, clientOutW := io.Pipe()
	serverInR, serverInW := io.Pipe()
	serverOutR, serverOutW := io.Pipe()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	errc := make(chan error, 1)
	go func() {
		errc <- proxy.ProxyStreams(ctx, clientInR, clientOutW, serverInW, serverOutR)
	}()

	line := "not json at all\n"
	go func() { _, _ = clientInW.Write([]byte(line)) }()

	buf := make([]byte, 1024)
	n, _ := serverInR.Read(buf)
	if string(buf[:n]) != line {
		t.Errorf("server received %q, want %q", string(buf[:n]), line)
	}

	serverLine := "server response not json\n"
	go func() { _, _ = serverOutW.Write([]byte(serverLine)) }()

	n, _ = clientOutR.Read(buf)
	if string(buf[:n]) != serverLine {
		t.Errorf("client received %q, want %q", string(buf[:n]), serverLine)
	}

	_ = clientInW.Close()
	_ = serverOutW.Close()

	if err := <-errc; err != nil {
		t.Fatalf("ProxyStreams: %v", err)
	}
}

func TestRunSSEBridge(t *testing.T) {
	st := openTemp(t)
	runID := "mcp-sse-test"

	sseMsgCh := make(chan string, 10)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher, _ := w.(http.Flusher)
			_, _ = fmt.Fprintf(w, "event: endpoint\ndata: /messages?session=abc\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			for {
				select {
				case msg := <-sseMsgCh:
					_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
					if flusher != nil {
						flusher.Flush()
					}
				case <-r.Context().Done():
					return
				}
			}
		case http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), "tools/call") {
				sseMsgCh <- `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"pong"}],"isError":false}}`
			}
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rIn, wIn, _ := os.Pipe()
	rOut, wOut, _ := os.Pipe()
	origStdin := os.Stdin
	origStdout := os.Stdout
	os.Stdin = rIn
	os.Stdout = wOut
	defer func() {
		os.Stdin = origStdin
		os.Stdout = origStdout
		_ = rIn.Close()
		_ = wIn.Close()
		_ = rOut.Close()
		_ = wOut.Close()
	}()

	bridgeDone := make(chan error, 1)
	go func() {
		bridgeDone <- RunSSEBridge(ctx, st, Options{RunID: runID, Capture: true}, ts.URL)
	}()

	req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ping","arguments":{}}}` + "\n"
	_, _ = wIn.Write([]byte(req))

	buf := make([]byte, 1024)
	n, err := rOut.Read(buf)
	if err != nil {
		t.Fatalf("read response from stdout: %v", err)
	}
	if !strings.Contains(string(buf[:n]), "pong") {
		t.Fatalf("unexpected response from bridge: %s", string(buf[:n]))
	}

	cancel()
	<-bridgeDone

	spans, err := st.Spans(context.Background(), runID)
	if err != nil {
		t.Fatalf("Spans: %v", err)
	}
	foundTool := false
	for _, sp := range spans {
		if sp.Kind == store.KindTool && sp.Name == "ping" && sp.Status == "ok" {
			foundTool = true
		}
	}
	if !foundTool {
		t.Errorf("spans missing ping tool call: %+v", spans)
	}
}

func TestRunStdioProcess(t *testing.T) {
	st := openTemp(t)
	runID := "mcp-stdio-proc"

	rIn, wIn, _ := os.Pipe()
	rOut, wOut, _ := os.Pipe()
	origStdin := os.Stdin
	origStdout := os.Stdout
	os.Stdin = rIn
	os.Stdout = wOut
	defer func() {
		os.Stdin = origStdin
		os.Stdout = origStdout
		_ = rIn.Close()
		_ = wIn.Close()
		_ = rOut.Close()
		_ = wOut.Close()
	}()

	cmdScript := `read line; echo '{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"hello from proc"}],"isError":false}}'`
	runErr := make(chan error, 1)
	go func() {
		runErr <- RunStdio(context.Background(), st, Options{RunID: runID, Capture: true}, []string{"sh", "-c", cmdScript})
	}()

	req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"greet","arguments":{"target":"world"}}}` + "\n"
	_, _ = wIn.Write([]byte(req))

	buf := make([]byte, 1024)
	n, err := rOut.Read(buf)
	if err != nil {
		t.Fatalf("rOut read: %v", err)
	}
	if !strings.Contains(string(buf[:n]), "hello from proc") {
		t.Fatalf("unexpected output: %s", string(buf[:n]))
	}

	_ = wIn.Close()
	if err := <-runErr; err != nil {
		t.Fatalf("RunStdio: %v", err)
	}

	spans, err := st.Spans(context.Background(), runID)
	if err != nil {
		t.Fatalf("Spans: %v", err)
	}
	foundGreet := false
	for _, sp := range spans {
		if sp.Kind == store.KindTool && sp.Name == "greet" && sp.Status == "ok" {
			foundGreet = true
		}
	}
	if !foundGreet {
		t.Errorf("spans missing greet tool call: %+v", spans)
	}
}
