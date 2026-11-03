//go:build ignore

// Run with: go run scripts/benchmark_otlp.go -db /tmp/capybara-otlp-bench.db
// Exercises HTTP OTLP decoding and SQLite persistence with a paced sender.
package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"

	"github.com/tonquoc0407/capybara/internal/ingest/otlp"
	"github.com/tonquoc0407/capybara/internal/store"
)

func main() {
	path := flag.String("db", "/tmp/capybara-otlp-bench.db", "fresh benchmark database")
	rate := flag.Int("rate", 500, "spans per second")
	duration := flag.Duration("duration", 10*time.Minute, "paced ingest duration")
	batch := flag.Int("batch", 50, "spans per OTLP request")
	flag.Parse()
	if *rate < 1 || *batch < 1 || *duration <= 0 || *rate%*batch != 0 {
		panic("invalid rate, duration or batch")
	}
	if _, err := os.Stat(*path); err == nil {
		panic("database already exists; choose a fresh -db")
	}
	st, err := store.Open(*path)
	if err != nil {
		panic(err)
	}
	defer st.Close()
	receiver := otlp.New(st, false)
	receiver.HTTPAddr, receiver.GRPCAddr = "127.0.0.1:0", "127.0.0.1:0"
	if err := receiver.Listen(); err != nil {
		panic(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- receiver.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			panic(err)
		}
	}()
	client := &http.Client{Timeout: 10 * time.Second}
	requests := int((*duration).Seconds()) * *rate / *batch
	started := time.Now()
	retries := 0
	for n := 0; n < requests; n++ {
		deadline := started.Add(time.Duration(int64(n+1) * int64(*batch) * int64(time.Second) / int64(*rate)))
		if wait := time.Until(deadline); wait > 0 {
			time.Sleep(wait)
		}
		body := makeRequest(n, *batch)
		for {
			req, err := http.NewRequestWithContext(ctx, "POST", receiver.HTTPEndpoint(), bytes.NewReader(body))
			if err != nil {
				panic(err)
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				panic(err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 200 {
				break
			}
			if resp.StatusCode != 503 {
				panic(fmt.Sprintf("OTLP HTTP returned %d", resp.StatusCode))
			}
			retries++
			time.Sleep(time.Second)
		}
	}
	elapsed := time.Since(started).Seconds()
	db, err := sql.Open("sqlite", "file:"+*path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		panic(err)
	}
	defer db.Close()
	var stored int
	if err := db.QueryRow("SELECT COUNT(*) FROM spans").Scan(&stored); err != nil {
		panic(err)
	}
	result := map[string]any{
		"expected_spans": requests * *batch, "stored_spans": stored,
		"elapsed_seconds": elapsed, "achieved_spans_per_second": float64(stored) / elapsed,
		"retry_responses": retries, "gomaxprocs": runtime.GOMAXPROCS(0), "cpu_count": runtime.NumCPU(),
		"rss_bytes": rss(), "db_bytes": dbSize(*path),
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		panic(err)
	}
	if stored != requests**batch {
		os.Exit(1)
	}
}

func rss() int64 {
	raw, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return -1
	}
	fields := strings.Fields(string(raw))
	if len(fields) < 2 {
		return -1
	}
	pages, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return -1
	}
	return pages * int64(os.Getpagesize())
}

func dbSize(path string) int64 {
	var size int64
	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		if info, err := os.Stat(name); err == nil {
			size += info.Size()
		}
	}
	return size
}

func makeRequest(batchNumber, size int) []byte {
	req := ptraceotlp.NewExportRequest()
	var traceID pcommon.TraceID
	binary.BigEndian.PutUint64(traceID[8:], uint64(batchNumber+1))
	spans := req.Traces().ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans()
	start := pcommon.Timestamp(time.Now().UnixNano())
	for i := range size {
		sp := spans.AppendEmpty()
		sp.SetTraceID(traceID)
		var spanID pcommon.SpanID
		binary.BigEndian.PutUint64(spanID[:], uint64(batchNumber*size+i+1))
		sp.SetSpanID(spanID)
		sp.SetName("synthetic")
		sp.SetStartTimestamp(start)
		sp.SetEndTimestamp(start + pcommon.Timestamp(20*time.Millisecond))
		sp.Attributes().PutStr("gen_ai.operation.name", "chat")
		sp.Attributes().PutInt("gen_ai.usage.input_tokens", 10)
		sp.Attributes().PutInt("gen_ai.usage.output_tokens", 5)
	}
	raw, err := req.MarshalJSON()
	if err != nil {
		panic(err)
	}
	return raw
}
