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

// MarkAnalyzed flags spans as processed by the analyzer.
func (s *Store) MarkAnalyzed(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	args := make([]any, len(ids))
	ph := make([]string, len(ids))
	for i, id := range ids {
		args[i] = id
		ph[i] = "?"
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE spans SET analyzed = 1 WHERE id IN (`+strings.Join(ph, ",")+`)`,
		args...); err != nil {
		return fmt.Errorf("mark analyzed: %w", err)
	}
	return nil
}
