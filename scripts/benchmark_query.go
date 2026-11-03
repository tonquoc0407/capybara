//go:build ignore

// Run against a filled benchmark DB: go run scripts/benchmark_query.go -db /tmp/capybara-bench.db
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

func main() {
	path := flag.String("db", "", "benchmark database")
	flag.Parse()
	if *path == "" {
		panic("-db is required")
	}
	if _, err := os.Stat(*path); err != nil {
		panic(err)
	}
	st, err := store.Open(*path)
	if err != nil {
		panic(err)
	}
	defer st.Close()
	db, err := sql.Open("sqlite", "file:"+*path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		panic(err)
	}
	defer db.Close()
	var spans, runs int
	if err := db.QueryRow("SELECT COUNT(*) FROM spans").Scan(&spans); err != nil {
		panic(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM runs").Scan(&runs); err != nil {
		panic(err)
	}
	if runs == 0 {
		panic("database has no runs")
	}
	pages := (runs + 49) / 50
	ctx := context.Background()
	listTimes, openTimes := make([]float64, 0, 100), make([]float64, 0, 100)
	for i := range 100 {
		offset := i * (pages - 1) / 99 * 50
		started := time.Now()
		items, err := st.ListRunsPage(ctx, "default", 50, offset)
		if err != nil || len(items) == 0 {
			panic(fmt.Sprintf("list at %d: %v", offset, err))
		}
		listTimes = append(listTimes, float64(time.Since(started))/float64(time.Millisecond))
		started = time.Now()
		if _, err := st.Spans(ctx, items[0].ID); err != nil {
			panic(err)
		}
		openTimes = append(openTimes, float64(time.Since(started))/float64(time.Millisecond))
	}
	sort.Float64s(listTimes)
	sort.Float64s(openTimes)
	var newest int64
	if err := db.QueryRow("SELECT MAX(started_at) FROM runs WHERE project = 'default'").Scan(&newest); err != nil {
		panic(err)
	}
	to := time.Unix(0, newest).Add(time.Nanosecond)
	operationsTimes := make([]float64, 0, 10)
	for range 10 {
		started := time.Now()
		if _, err := st.Operations(ctx, store.OperationsFilter{Project: "default", From: to.Add(-24 * time.Hour), To: to}); err != nil {
			panic(err)
		}
		operationsTimes = append(operationsTimes, float64(time.Since(started))/float64(time.Millisecond))
	}
	sort.Float64s(operationsTimes)
	result := map[string]any{"spans": spans, "runs": runs, "samples": 100,
		"list_p95_ms": listTimes[94], "open_run_p95_ms": openTimes[94],
		"operations_samples": 10, "operations_p95_ms": operationsTimes[9],
		"deepest_offset": (pages - 1) * 50}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		panic(err)
	}
}
