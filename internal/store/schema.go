package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ToolSchema is one row of tool_schemas: one learned or declared version of a
// tool's output shape.
type ToolSchema struct {
	ToolName       string
	Version        int64
	Schema         string
	LearnedFromRun string
	FirstSeen      time.Time
	LastSeen       time.Time
}

// LatestToolSchema returns the current schema version for a tool, or nil.
func (s *Store) LatestToolSchema(ctx context.Context, tool string) (*ToolSchema, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT tool_name, version, schema_json, learned_from_run, first_seen, last_seen
		 FROM tool_schemas WHERE tool_name = ? ORDER BY version DESC LIMIT 1`, tool)
	var ts ToolSchema
	var learned sql.NullString
	var first, last sql.NullInt64
	err := row.Scan(&ts.ToolName, &ts.Version, &ts.Schema, &learned, &first, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("latest schema of %s: %w", tool, err)
	}
	ts.LearnedFromRun = learned.String
	ts.FirstSeen = fromNanos(first)
	ts.LastSeen = fromNanos(last)
	return &ts, nil
}

// InsertToolSchema records a new schema version.
func (s *Store) InsertToolSchema(ctx context.Context, ts ToolSchema) error {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO tool_schemas (tool_name, version, schema_json, learned_from_run,
		                           first_seen, last_seen)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		ts.ToolName, ts.Version, ts.Schema, nullString(ts.LearnedFromRun),
		nullNanos(ts.FirstSeen), nullNanos(ts.LastSeen)); err != nil {
		return fmt.Errorf("insert schema %s v%d: %w", ts.ToolName, ts.Version, err)
	}
	return nil
}

// TouchToolSchema widens a schema version in place and bumps last_seen.
func (s *Store) TouchToolSchema(ctx context.Context, tool string, version int64, schema string, lastSeen time.Time) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE tool_schemas SET schema_json = ?, last_seen = ?
		 WHERE tool_name = ? AND version = ?`,
		schema, nullNanos(lastSeen), tool, version); err != nil {
		return fmt.Errorf("touch schema %s v%d: %w", tool, version, err)
	}
	return nil
}

// UnanalyzedSpans returns completed spans not yet analyzed, oldest first, so
// re-analysis replays the original learning order.
func (s *Store) UnanalyzedSpans(ctx context.Context) ([]Span, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, run_id, parent_id, kind, name, started_at, ended_at,
		        tokens_in, tokens_out, cost_usd, status, attrs_json
		 FROM spans WHERE analyzed = 0 AND ended_at IS NOT NULL
		 ORDER BY ended_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list unanalyzed: %w", err)
	}
	defer rows.Close()
	return scanSpans(rows)
}

// ResetAnalysis drops every derived finding and taint, forgets the learned tool
// schemas, and marks all spans unanalyzed, so the next sweep re-evaluates the
// whole store from scratch. Dropping the schemas is what makes the re-sweep
// replay the original learning order: keep them and drift would diff a call
// against a shape a later call taught, flagging the wrong span. Spans, their
// content, and declared schemas (which live on span attributes) are untouched.
func (s *Store) ResetAnalysis(ctx context.Context) error {
	for _, stmt := range []string{
		"DELETE FROM findings",
		"DELETE FROM taints",
		"DELETE FROM tool_schemas",
		"UPDATE spans SET analyzed = 0",
	} {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("reset analysis: %w", err)
		}
	}
	return nil
}

// markAnalyzedChunk stays below SQLite's lowest common bind-variable limit.
// A sweep can contain an entire imported trace, so one placeholder per span
// stops working long before a 100k-span trace is unusual.
const markAnalyzedChunk = 900

// MarkAnalyzed flags spans as processed by the analyzer. Chunks share one
// transaction, so a failure cannot leave a sweep only partly marked.
func (s *Store) MarkAnalyzed(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mark analyzed: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for start := 0; start < len(ids); start += markAnalyzedChunk {
		end := min(start+markAnalyzedChunk, len(ids))
		args := make([]any, end-start)
		ph := make([]string, end-start)
		for i, id := range ids[start:end] {
			args[i], ph[i] = id, "?"
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE spans SET analyzed = 1 WHERE id IN (`+strings.Join(ph, ",")+`)`,
			args...); err != nil {
			return fmt.Errorf("mark analyzed: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("mark analyzed: commit: %w", err)
	}
	return nil
}
