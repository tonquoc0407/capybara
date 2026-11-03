package store

import (
	"context"
	"database/sql"
	"time"
)

// AlertWindow summarizes only observed runs. Cost is the sum of known run
// costs; UnpricedRuns tells operators how incomplete that number is.
type AlertWindow struct {
	Project      string  `json:"project"`
	Since        string  `json:"since"`
	Runs         int64   `json:"runs"`
	Errors       int64   `json:"errors"`
	CostUSD      float64 `json:"cost_usd"`
	UnpricedRuns int64   `json:"unpriced_runs"`
	P95LatencyMS float64 `json:"p95_latency_ms"`
}

func (s *Store) RunAlertWindow(ctx context.Context, project string, since time.Time) (AlertWindow, error) {
	result := AlertWindow{Project: project, Since: since.UTC().Format(time.RFC3339)}
	var cost sql.NullFloat64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END), 0),
		SUM(cost_usd),
		COALESCE(SUM(CASE WHEN cost_usd IS NULL THEN 1 ELSE 0 END), 0)
		FROM runs WHERE project = ? AND started_at >= ?`, project, since.UnixNano()).Scan(
		&result.Runs, &result.Errors, &cost, &result.UnpricedRuns)
	if err != nil {
		return result, err
	}
	result.CostUSD = cost.Float64
	var completed int64
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs WHERE project = ? AND started_at >= ?
		AND status != 'running' AND ended_at >= started_at`, project, since.UnixNano()).Scan(&completed)
	if err != nil || completed == 0 {
		return result, err
	}
	index := (95*completed+99)/100 - 1 // nearest-rank p95, zero based
	var nanos int64
	err = s.db.QueryRowContext(ctx, `SELECT ended_at - started_at FROM runs
		WHERE project = ? AND started_at >= ? AND status != 'running' AND ended_at >= started_at
		ORDER BY ended_at - started_at, id LIMIT 1 OFFSET ?`,
		project, since.UnixNano(), index).Scan(&nanos)
	result.P95LatencyMS = float64(nanos) / 1e6
	return result, err
}
