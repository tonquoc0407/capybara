package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"
)

// OperationsFilter selects one project and a bounded time range for reporting.
type OperationsFilter struct {
	Project, Environment, Release string
	From, To                      time.Time
}

// OperationsGroup holds counts, known cost, and latency for a release or hour.
type OperationsGroup struct {
	Key              string   `json:"key"`
	Runs             int64    `json:"runs"`
	Errors           int64    `json:"errors"`
	Running          int64    `json:"running"`
	Completed        int64    `json:"completed"`
	KnownCostUSD     float64  `json:"known_cost_usd"`
	UnpricedRuns     int64    `json:"unpriced_runs"`
	UnpricedLLMSpans int64    `json:"unpriced_llm_spans"`
	P95LatencyMS     *float64 `json:"p95_latency_ms"`
}

// OperationsQuality holds the latest scored observations for a release and metric.
type OperationsQuality struct {
	Release    string  `json:"release"`
	Name       string  `json:"name"`
	Samples    int64   `json:"samples"`
	ScoredRuns int64   `json:"scored_runs"`
	Average    float64 `json:"average"`
}

// OperationsReport combines totals, release and hourly groups, and quality scores.
type OperationsReport struct {
	Project     string              `json:"project"`
	Environment string              `json:"environment"`
	Release     string              `json:"release"`
	From        time.Time           `json:"from"`
	To          time.Time           `json:"to"`
	Total       OperationsGroup     `json:"total"`
	Releases    []OperationsGroup   `json:"releases"`
	Hourly      []OperationsGroup   `json:"hourly"`
	Quality     []OperationsQuality `json:"quality"`
}

// Validate requires a project and a time range no longer than 31 days.
func (f OperationsFilter) Validate() error {
	if f.Project == "" || f.From.IsZero() || f.To.IsZero() || !f.From.Before(f.To) || f.To.Sub(f.From) > 31*24*time.Hour {
		return errors.New("operations requires a project and an increasing time range of at most 31 days")
	}
	return nil
}

// Operations reports observed runs in [From, To). Running or invalid-duration
// runs do not enter latency percentiles. Costs include only known amounts.
// Quality uses the latest score per run/item/name, keeping metrics separate.
func (s *Store) Operations(ctx context.Context, f OperationsFilter) (OperationsReport, error) {
	report := OperationsReport{
		Project: f.Project, Environment: f.Environment, Release: f.Release,
		From: f.From.UTC(), To: f.To.UTC(), Releases: []OperationsGroup{}, Hourly: []OperationsGroup{}, Quality: []OperationsQuality{},
	}
	if err := f.Validate(); err != nil {
		return report, err
	}
	query := `WITH filtered AS (
		SELECT r.id, r.release, r.started_at, r.ended_at, r.status, r.cost_usd
		FROM runs r WHERE r.project = ? AND r.started_at >= ? AND r.started_at < ?`
	args := []any{f.Project, f.From.UnixNano(), f.To.UnixNano()}
	if f.Environment != "" {
		query += ` AND r.environment = ?`
		args = append(args, f.Environment)
	}
	if f.Release != "" {
		query += ` AND r.release = ?`
		args = append(args, f.Release)
	}
	query += `)`
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return report, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, query+`
	SELECT r.release, r.started_at, r.ended_at, r.status, r.cost_usd,
		(SELECT COUNT(*) FROM spans sp WHERE sp.run_id = r.id AND sp.kind = 'llm' AND sp.cost_usd IS NULL)
	FROM filtered r`, args...)
	if err != nil {
		return report, err
	}
	// ponytail: exact p95 retains three int64 durations per completed run;
	// use time-bucket rollups if measured windows exceed the memory budget.
	total := &operationsAccumulator{}
	releases, hours := map[string]*operationsAccumulator{}, map[string]*operationsAccumulator{}
	groupFor := func(groups map[string]*operationsAccumulator, key string) *operationsAccumulator {
		if groups[key] == nil {
			groups[key] = &operationsAccumulator{OperationsGroup: OperationsGroup{Key: key}}
		}
		return groups[key]
	}
	for rows.Next() {
		var release, status string
		var started, unpriced int64
		var ended sql.NullInt64
		var cost sql.NullFloat64
		if err := rows.Scan(&release, &started, &ended, &status, &cost, &unpriced); err != nil {
			rows.Close()
			return report, err
		}
		var duration *int64
		if elapsed := ended.Int64 - started; status != "running" && ended.Valid && ended.Int64 >= started && elapsed >= 0 {
			duration = &elapsed
		}
		hour := time.Unix(0, started).UTC().Truncate(time.Hour).Format(time.RFC3339)
		for _, group := range []*operationsAccumulator{total, groupFor(releases, release), groupFor(hours, hour)} {
			group.add(status, cost, unpriced, duration)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return report, err
	}
	report.Total = total.result()
	for _, entry := range []struct {
		groups map[string]*operationsAccumulator
		dst    *[]OperationsGroup
	}{{releases, &report.Releases}, {hours, &report.Hourly}} {
		keys := make([]string, 0, len(entry.groups))
		for key := range entry.groups {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			*entry.dst = append(*entry.dst, entry.groups[key].result())
		}
	}
	rows, err = tx.QueryContext(ctx, query+`, latest AS (
		SELECT r.release, sc.run_id, sc.name, sc.value,
		ROW_NUMBER() OVER (PARTITION BY sc.run_id, COALESCE(sc.item_id, ''), sc.name
			ORDER BY sc.created_at DESC, sc.id DESC) AS rank
		FROM scores sc JOIN filtered r ON r.id = sc.run_id
	)
	SELECT release, name, COUNT(*), COUNT(DISTINCT run_id), AVG(value)
	FROM latest WHERE rank = 1 GROUP BY release, name ORDER BY release, name`, args...)
	if err != nil {
		return report, err
	}
	for rows.Next() {
		var q OperationsQuality
		if err := rows.Scan(&q.Release, &q.Name, &q.Samples, &q.ScoredRuns, &q.Average); err != nil {
			rows.Close()
			return report, err
		}
		report.Quality = append(report.Quality, q)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return report, err
	}
	return report, tx.Commit()
}

type operationsAccumulator struct {
	OperationsGroup
	durations []int64
}

func (g *operationsAccumulator) add(status string, cost sql.NullFloat64, unpriced int64, duration *int64) {
	g.Runs++
	if status == "error" {
		g.Errors++
	}
	if status == "running" {
		g.Running++
	}
	if cost.Valid {
		g.KnownCostUSD += cost.Float64
	} else {
		g.UnpricedRuns++
	}
	g.UnpricedLLMSpans += unpriced
	if duration != nil {
		g.Completed++
		g.durations = append(g.durations, *duration)
	}
}

func (g *operationsAccumulator) result() OperationsGroup {
	if n := len(g.durations); n > 0 {
		sort.Slice(g.durations, func(i, j int) bool { return g.durations[i] < g.durations[j] })
		p95 := float64(g.durations[(95*n+99)/100-1]) / 1e6
		g.P95LatencyMS = &p95
	}
	return g.OperationsGroup
}
