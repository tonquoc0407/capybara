package store

import (
	"context"
	"database/sql"
	"fmt"
)

// PutTraceLogs is idempotent. Logs can precede their spans and even the trace.
func (s *Store) PutTraceLogs(ctx context.Context, source string, logs []TraceLog) error {
	return s.putTraceLogs(ctx, source, "", logs)
}

func (s *Store) PutProjectTraceLogs(ctx context.Context, source, project string, logs []TraceLog) error {
	if project == "" {
		return fmt.Errorf("project is required")
	}
	return s.putTraceLogs(ctx, source, project, logs)
}

func (s *Store) putTraceLogs(ctx context.Context, source, project string, logs []TraceLog) error {
	if len(logs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, l := range logs {
		owner := project
		if owner == "" {
			owner = "default"
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO runs (id, source, project) VALUES (?, ?, ?)
			ON CONFLICT (id) DO NOTHING`, l.RunID, source, owner); err != nil {
			return fmt.Errorf("create run for log: %w", err)
		}
		if project != "" {
			if err := tx.QueryRowContext(ctx, `SELECT project FROM runs WHERE id = ?`, l.RunID).Scan(&owner); err != nil {
				return err
			}
			if owner != project {
				return fmt.Errorf("run %s belongs to another project", l.RunID)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO trace_logs
			(id, run_id, span_id, observed_at, severity, body, attrs_json)
			VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT (id) DO NOTHING`,
			l.ID, l.RunID, l.SpanID, nullNanos(l.ObservedAt), l.Severity, l.Body, l.AttrsJSON); err != nil {
			return fmt.Errorf("insert trace log %s: %w", l.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.notify()
	return nil
}

func (s *Store) TraceLogs(ctx context.Context, runID string) ([]TraceLog, error) {
	return s.traceLogs(ctx, runID, true)
}

// TraceLogHeaders lists linked logs without reading potentially large bodies.
func (s *Store) TraceLogHeaders(ctx context.Context, runID string) ([]TraceLog, error) {
	return s.traceLogs(ctx, runID, false)
}

func (s *Store) traceLogs(ctx context.Context, runID string, withContent bool) ([]TraceLog, error) {
	query := `SELECT id, run_id, span_id, observed_at, severity, '', ''
		FROM trace_logs WHERE run_id = ? ORDER BY observed_at, id`
	if withContent {
		query = `SELECT id, run_id, span_id, observed_at, severity, body, attrs_json
			FROM trace_logs WHERE run_id = ? ORDER BY observed_at, id`
	}
	rows, err := s.db.QueryContext(ctx, query, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TraceLog
	for rows.Next() {
		var l TraceLog
		var at sql.NullInt64
		if err := rows.Scan(&l.ID, &l.RunID, &l.SpanID, &at, &l.Severity, &l.Body, &l.AttrsJSON); err != nil {
			return nil, err
		}
		l.ObservedAt = fromNanos(at)
		out = append(out, l)
	}
	return out, rows.Err()
}
