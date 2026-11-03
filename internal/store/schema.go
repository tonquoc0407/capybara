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
	Project        string
	ToolName       string
	Version        int64
	Schema         string
	LearnedFromRun string
	FirstSeen      time.Time
	LastSeen       time.Time
}

// LatestToolSchema returns the current schema version for a tool, or nil.
func (s *Store) LatestToolSchema(ctx context.Context, tool string) (*ToolSchema, error) {
	return s.LatestProjectToolSchema(ctx, "default", tool)
}

// LatestProjectToolSchema returns the newest output schema within one project.
func (s *Store) LatestProjectToolSchema(ctx context.Context, project, tool string) (*ToolSchema, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT project, tool_name, version, schema_json, learned_from_run, first_seen, last_seen
		 FROM tool_schemas WHERE project = ? AND tool_name = ? ORDER BY version DESC LIMIT 1`, project, tool)
	var ts ToolSchema
	var learned sql.NullString
	var first, last sql.NullInt64
	err := row.Scan(&ts.Project, &ts.ToolName, &ts.Version, &ts.Schema, &learned, &first, &last)
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
	if ts.Project == "" {
		ts.Project = "default"
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO tool_schemas (project, tool_name, version, schema_json, learned_from_run,
		                           first_seen, last_seen)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		ts.Project, ts.ToolName, ts.Version, ts.Schema, nullString(ts.LearnedFromRun),
		nullNanos(ts.FirstSeen), nullNanos(ts.LastSeen)); err != nil {
		return fmt.Errorf("insert schema %s v%d: %w", ts.ToolName, ts.Version, err)
	}
	return nil
}

// TouchToolSchema widens a schema version in place and bumps last_seen.
func (s *Store) TouchToolSchema(ctx context.Context, tool string, version int64, schema string, lastSeen time.Time) error {
	return s.TouchProjectToolSchema(ctx, "default", tool, version, schema, lastSeen)
}

// TouchProjectToolSchema updates a version's learned shape and last-seen time.
func (s *Store) TouchProjectToolSchema(ctx context.Context, project, tool string, version int64, schema string, lastSeen time.Time) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE tool_schemas SET schema_json = ?, last_seen = ?
		 WHERE project = ? AND tool_name = ? AND version = ?`,
		schema, nullNanos(lastSeen), project, tool, version); err != nil {
		return fmt.Errorf("touch schema %s v%d: %w", tool, version, err)
	}
	return nil
}

// UnanalyzedSpans returns completed spans not yet analyzed, oldest first, so
// re-analysis replays the original learning order.
func (s *Store) UnanalyzedSpans(ctx context.Context) ([]Span, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, run_id, parent_id, kind, name, started_at, ended_at,
		        tokens_in, tokens_out, cost_usd, status, attrs_json, revision
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

// MarkAnalyzed acknowledges only the revision that was actually analyzed. A
// late write during a sweep stays pending for the next pass.
func (s *Store) MarkAnalyzed(ctx context.Context, spans []Span) error {
	if len(spans) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for first := 0; first < len(spans); first += 256 {
		batch := spans[first:min(first+256, len(spans))]
		args, placeholders := make([]any, 0, 2*len(batch)), make([]string, 0, len(batch))
		for _, sp := range batch {
			args = append(args, sp.ID, sp.Revision)
			placeholders = append(placeholders, "(?, ?)")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE spans SET analyzed = 1
			WHERE (id, revision) IN (VALUES `+strings.Join(placeholders, ",")+`)`, args...); err != nil {
			return fmt.Errorf("mark analyzed: %w", err)
		}
	}
	return tx.Commit()
}
