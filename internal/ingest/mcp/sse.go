package mcp

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/store"
)

// RunSSEBridge connects to an MCP SSE server at sseURL, providing a stdio-to-SSE bridge.
func RunSSEBridge(ctx context.Context, st *store.Store, opts Options, sseURL string) error {
	runID := opts.RunID
	if runID == "" {
		runID = os.Getenv("CAPYBARA_RUN_ID")
	}
	if runID == "" {
		runID = "mcp-sse-" + randomHex(8)
	}
	rootSpanID := "root-" + randomHex(8)
	name := opts.Name
	if name == "" {
		name = "mcp-sse: " + sseURL
	}

	rootSpan := store.Span{
		ID:        rootSpanID,
		RunID:     runID,
		Kind:      store.KindAgent,
		Name:      name,
		StartedAt: time.Now().UTC(),
		Status:    "running",
		Attrs: store.Attrs{
			MCP: true,
			Raw: map[string]any{
				"sse_url": sseURL,
			},
		},
	}
	_ = st.WriteBatch(ctx, store.Batch{
		Source: "mcp",
		Spans:  []store.Span{rootSpan},
	})

	req, err := http.NewRequestWithContext(ctx, "GET", sseURL, nil)
	if err != nil {
		return fmt.Errorf("sse request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connect to sse %s: %w", sseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sse connect status %d: %s", resp.StatusCode, resp.Status)
	}

	parsedBase, err := url.Parse(sseURL)
	if err != nil {
		return fmt.Errorf("parse sse url: %w", err)
	}

	serverOutR, serverOutW := io.Pipe()
	endpointCh := make(chan string, 1)

	go func() {
		defer serverOutW.Close()
		scanner := bufio.NewScanner(resp.Body)
		var currentEvent string
		endpointSent := false

		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event:") {
				currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			} else if strings.HasPrefix(line, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if currentEvent == "endpoint" && !endpointSent {
					endpointSent = true
					endpointCh <- data
				} else if currentEvent == "message" || currentEvent == "" {
					_, _ = serverOutW.Write([]byte(data + "\n"))
				}
			} else if line == "" {
				currentEvent = ""
			}
		}
		if !endpointSent {
			endpointCh <- ""
		}
	}()

	var postEndpoint string
	select {
	case ep := <-endpointCh:
		if ep == "" {
			return fmt.Errorf("sse server did not provide an endpoint event")
		}
		rel, err := url.Parse(ep)
		if err != nil {
			return fmt.Errorf("parse endpoint %q: %w", ep, err)
		}
		postEndpoint = parsedBase.ResolveReference(rel).String()
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(10 * time.Second):
		return fmt.Errorf("timeout waiting for sse endpoint event")
	}

	serverIn := &ssePostWriter{
		ctx:     ctx,
		postURL: postEndpoint,
		client:  client,
	}

	proxy := NewStreamProxy(st, runID, rootSpanID, opts.Capture)
	proxyErr := proxy.ProxyStreams(ctx, os.Stdin, os.Stdout, serverIn, serverOutR)

	rootSpan.EndedAt = time.Now().UTC()
	if proxyErr != nil {
		rootSpan.Status = "error"
	} else {
		rootSpan.Status = "ok"
	}
	_ = st.WriteBatch(ctx, store.Batch{
		Source: "mcp",
		Spans:  []store.Span{rootSpan},
	})

	an, err := analyze.New(st)
	if err == nil {
		_ = an.Sweep(ctx)
	}

	return proxyErr
}

type ssePostWriter struct {
	ctx     context.Context
	postURL string
	client  *http.Client
}

func (w *ssePostWriter) Write(p []byte) (int, error) {
	trimmed := bytes.TrimSpace(p)
	if len(trimmed) == 0 {
		return len(p), nil
	}
	req, err := http.NewRequestWithContext(w.ctx, "POST", w.postURL, bytes.NewReader(trimmed))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("mcp post status %d", resp.StatusCode)
	}
	return len(p), nil
}
