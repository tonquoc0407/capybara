//go:build ignore

// Run with: go run scripts/benchmark.go -db /tmp/capybara-bench.db
// This is a repeatable local load test, not part of the shipped binary.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

type report struct {
	StreamingSpans   int     `json:"streaming_spans"`
	StreamingSeconds float64 `json:"streaming_seconds"`
	SpansPerSecond   float64 `json:"spans_per_second"`
	StoredSpans      int64   `json:"stored_spans"`
	ExpectedSpans    int     `json:"expected_spans"`
	ListP95MS        float64 `json:"list_p95_ms"`
	OpenRunP95MS     float64 `json:"open_run_p95_ms"`
	RSSBytes         int64   `json:"rss_bytes"`
	DBBytes          int64   `json:"db_bytes"`
	CPUCount         int     `json:"cpu_count"`
}

func main() {
	path := flag.String("db", "/tmp/capybara-bench.db", "fresh benchmark database")
	rate := flag.Int("rate", 500, "streaming spans per second")
	stream := flag.Int("stream-spans", 300000, "number of paced spans")
	target := flag.Int("target-spans", 1000000, "total spans before query measurement")
	batchSize := flag.Int("batch", 50, "spans per transactional batch")
	flag.Parse()
	if *rate < 1 || *batchSize < 1 || *stream < 1 || *target < *stream || *stream%*batchSize != 0 || *target%*batchSize != 0 {
		panic("invalid rate, batch, or span counts")
	}
	if _, err := os.Stat(*path); err == nil {
		panic("benchmark database already exists; choose a fresh -db path")
	}
	st, err := store.Open(*path)
	if err != nil {
		panic(err)
	}
	defer st.Close()
	ctx := context.Background()
	started := time.Now()
	var streamSeconds float64
	for first := 0; first < *target; first += *batchSize {
		if first < *stream {
			deadline := started.Add(time.Duration(int64(first+*batchSize) * int64(time.Second) / int64(*rate)))
			if wait := time.Until(deadline); wait > 0 {
				time.Sleep(wait)
			}
		}
		if err := st.WriteBatch(ctx, makeBatch(first, *batchSize)); err != nil {
			panic(err)
		}
		if first+*batchSize == *stream {
			streamSeconds = time.Since(started).Seconds()
			fmt.Fprintf(os.Stderr, "streamed %d spans in %s\n", *stream, time.Since(started).Round(time.Second))
		}
	}
	db, err := sql.Open("sqlite", "file:"+*path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		panic(err)
	}
	defer db.Close()
	var count int64
	if err := db.QueryRow("SELECT COUNT(*) FROM spans").Scan(&count); err != nil {
		panic(err)
	}
	listTimes, openTimes := make([]float64, 0, 100), make([]float64, 0, 100)
	pages := max(1, (*target / *batchSize + 49)/50)
	for i := range 100 {
		start := time.Now()
		runs, err := st.ListRunsPage(ctx, "default", 50, (i%pages)*50)
		if err != nil || len(runs) == 0 {
			panic(fmt.Sprintf("list query: %v", err))
		}
		listTimes = append(listTimes, float64(time.Since(start))/float64(time.Millisecond))
		start = time.Now()
		if _, err := st.Spans(ctx, runs[0].ID); err != nil {
			panic(err)
		}
		openTimes = append(openTimes, float64(time.Since(start))/float64(time.Millisecond))
	}
	result := report{StreamingSpans: *stream, StreamingSeconds: streamSeconds,
		SpansPerSecond: float64(*stream) / streamSeconds,
		StoredSpans:    count, ExpectedSpans: *target, ListP95MS: p95(listTimes), OpenRunP95MS: p95(openTimes),
		RSSBytes: rss(), DBBytes: dbSize(*path), CPUCount: runtime.NumCPU()}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		panic(err)
	}
	if count != int64(*target) {
		os.Exit(1)
	}
}

func makeBatch(first, count int) store.Batch {
	runID := fmt.Sprintf("%032x", first/count+1)
	root := fmt.Sprintf("%016x", first+1)
	start := time.Now().UTC()
	b := store.Batch{Source: "benchmark", Spans: make([]store.Span, 0, count)}
	for i := range count {
		parent := root
		kind := store.KindLLM
		if i == 0 {
			parent = ""
			kind = store.KindAgent
		}
		b.Spans = append(b.Spans, store.Span{ID: fmt.Sprintf("%016x", first+i+1), RunID: runID,
			ParentID: parent, Kind: kind, Name: "synthetic", StartedAt: start,
			EndedAt: start.Add(20 * time.Millisecond), Status: "ok", TokensIn: 10, TokensOut: 5})
	}
	return b
}

func p95(values []float64) float64 {
	sort.Float64s(values)
	return values[(95*len(values)+99)/100-1]
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
	var total int64
	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		if info, err := os.Stat(name); err == nil {
			total += info.Size()
		}
	}
	return total
}
