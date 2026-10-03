// Package mcp proxies and traces Model Context Protocol (MCP) servers.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

// Options configures MCP proxy tracing.
type Options struct {
	RunID   string
	Name    string
	Capture bool
	Source  string
}

type inFlightCall struct {
	spanID    string
	kind      store.Kind
	name      string
	toolName  string
	input     string
	startedAt time.Time
}

type jsonRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// StreamProxy intercepts JSON-RPC messages between MCP client and server streams.
type StreamProxy struct {
	st               *store.Store
	runID            string
	parentSpanID     string
	capture          bool
	source           string
	mu               sync.Mutex
	inFlight         map[string]*inFlightCall
	inFlightSampling map[string]*inFlightCall
}

// NewStreamProxy creates a new MCP stream proxy interceptor.
func NewStreamProxy(st *store.Store, runID, parentSpanID string, capture bool) *StreamProxy {
	return &StreamProxy{
		st:               st,
		runID:            runID,
		parentSpanID:     parentSpanID,
		capture:          capture,
		source:           "mcp",
		inFlight:         make(map[string]*inFlightCall),
		inFlightSampling: make(map[string]*inFlightCall),
	}
}

// ProxyStreams pipes client <-> server streams and intercepts MCP messages in real-time.
func (p *StreamProxy) ProxyStreams(ctx context.Context, clientIn io.Reader, clientOut io.Writer, serverIn io.Writer, serverOut io.Reader) error {
	errc := make(chan error, 2)

	go func() {
		errc <- p.pipe(ctx, clientIn, serverIn, func(line []byte) {
			p.handleClientLine(ctx, line)
		})
	}()

	go func() {
		errc <- p.pipe(ctx, serverOut, clientOut, func(line []byte) {
			p.handleServerLine(ctx, line)
		})
	}()

	var firstErr error
	for i := 0; i < 2; i++ {
		select {
		case err := <-errc:
			if err != nil && firstErr == nil && !errors.Is(err, io.EOF) {
				firstErr = err
			}
		case <-ctx.Done():
			if firstErr == nil {
				firstErr = ctx.Err()
			}
		}
	}
	return firstErr
}

func (p *StreamProxy) pipe(ctx context.Context, src io.Reader, dst io.Writer, onLine func([]byte)) error {
	defer func() {
		if c, ok := dst.(io.Closer); ok {
			if f, ok := dst.(*os.File); !ok || (f != os.Stdout && f != os.Stderr) {
				_ = c.Close()
			}
		}
	}()
	r := bufio.NewReader(src)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			onLine(line)
			if _, werr := dst.Write(line); werr != nil {
				return werr
			}
			if flusher, ok := dst.(interface{ Flush() error }); ok {
				_ = flusher.Flush()
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func (p *StreamProxy) handleClientLine(ctx context.Context, line []byte) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return
	}
	var msg jsonRPCMessage
	if err := json.Unmarshal(trimmed, &msg); err != nil {
		return
	}
	idKey := string(msg.ID)

	// Response to server-initiated sampling
	if msg.Method == "" && len(msg.ID) > 0 && idKey != "null" {
		p.mu.Lock()
		call, ok := p.inFlightSampling[idKey]
		if ok {
			delete(p.inFlightSampling, idKey)
		}
		p.mu.Unlock()
		if ok {
			p.recordCompletedCall(ctx, call, msg)
		}
		return
	}

	if msg.Method == "" || len(msg.ID) == 0 || idKey == "null" {
		return
	}

	spanID := randomHex(8)
	now := time.Now().UTC()
	switch msg.Method {
	case "tools/call":
		var tp struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		_ = json.Unmarshal(msg.Params, &tp)
		toolName := tp.Name
		if toolName == "" {
			toolName = "tool"
		}
		inputBody := string(tp.Arguments)
		if inputBody == "" || inputBody == "null" {
			inputBody = string(msg.Params)
		}
		p.mu.Lock()
		p.inFlight[idKey] = &inFlightCall{
			spanID:    spanID,
			kind:      store.KindTool,
			name:      toolName,
			toolName:  toolName,
			input:     inputBody,
			startedAt: now,
		}
		p.mu.Unlock()

	case "resources/read":
		var rp struct {
			URI string `json:"uri"`
		}
		_ = json.Unmarshal(msg.Params, &rp)
		name := "read"
		if rp.URI != "" {
			name = "read " + rp.URI
		}
		p.mu.Lock()
		p.inFlight[idKey] = &inFlightCall{
			spanID:    spanID,
			kind:      store.KindRetrieval,
			name:      name,
			toolName:  "resources/read",
			input:     string(msg.Params),
			startedAt: now,
		}
		p.mu.Unlock()

	case "prompts/get":
		var pp struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(msg.Params, &pp)
		name := "prompt"
		if pp.Name != "" {
			name = "prompt " + pp.Name
		}
		p.mu.Lock()
		p.inFlight[idKey] = &inFlightCall{
			spanID:    spanID,
			kind:      store.KindRetrieval,
			name:      name,
			toolName:  "prompts/get",
			input:     string(msg.Params),
			startedAt: now,
		}
		p.mu.Unlock()
	}
}

func (p *StreamProxy) handleServerLine(ctx context.Context, line []byte) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return
	}
	var msg jsonRPCMessage
	if err := json.Unmarshal(trimmed, &msg); err != nil {
		return
	}
	idKey := string(msg.ID)

	// Server-initiated sampling request
	if msg.Method == "sampling/createMessage" && len(msg.ID) > 0 && idKey != "null" {
		spanID := randomHex(8)
		p.mu.Lock()
		p.inFlightSampling[idKey] = &inFlightCall{
			spanID:    spanID,
			kind:      store.KindLLM,
			name:      "sampling/createMessage",
			input:     string(msg.Params),
			startedAt: time.Now().UTC(),
		}
		p.mu.Unlock()
		return
	}

	if len(msg.ID) == 0 || idKey == "null" {
		return
	}
	p.mu.Lock()
	call, ok := p.inFlight[idKey]
	if ok {
		delete(p.inFlight, idKey)
	}
	p.mu.Unlock()
	if !ok {
		return
	}
	p.recordCompletedCall(ctx, call, msg)
}

func (p *StreamProxy) recordCompletedCall(ctx context.Context, call *inFlightCall, msg jsonRPCMessage) {
	if p.st == nil {
		return
	}
	endedAt := time.Now().UTC()
	status := "ok"
	outputBody := ""

	if msg.Error != nil {
		status = "error"
		errBytes, _ := json.Marshal(msg.Error)
		outputBody = string(errBytes)
	} else {
		outputBody = string(msg.Result)
		var resObj struct {
			IsError      bool `json:"isError"`
			IsErrorSnake bool `json:"is_error"`
		}
		if err := json.Unmarshal(msg.Result, &resObj); err == nil {
			if resObj.IsError || resObj.IsErrorSnake {
				status = "error"
			}
		}
	}

	sp := store.Span{
		ID:        call.spanID,
		RunID:     p.runID,
		ParentID:  p.parentSpanID,
		Kind:      call.kind,
		Name:      call.name,
		StartedAt: call.startedAt,
		EndedAt:   endedAt,
		Status:    status,
		Attrs: store.Attrs{
			ToolName: call.toolName,
			MCP:      true,
		},
	}

	var contents []store.Content
	if p.capture {
		if call.input != "" {
			contents = append(contents, store.Content{
				SpanID:    call.spanID,
				Role:      "input",
				Seq:       0,
				Body:      call.input,
				MediaType: "application/json",
			})
		}
		if outputBody != "" {
			contents = append(contents, store.Content{
				SpanID:    call.spanID,
				Role:      "output",
				Seq:       1,
				Body:      outputBody,
				MediaType: "application/json",
			})
		}
	}

	batch := store.Batch{
		Source:   p.source,
		Spans:    []store.Span{sp},
		Contents: contents,
	}
	_ = p.st.WriteBatch(ctx, batch)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
