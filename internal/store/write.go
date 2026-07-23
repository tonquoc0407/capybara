package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Batch is one transactional set of spans, contents and findings from a single source.
type Batch struct {
	Source   string
	Spans    []Span
	Contents []Content
	Findings []Finding
}

// WriteBatch inserts the batch atomically and refreshes the touched runs' aggregates.
func (s *Store) WriteBatch(ctx context.Context, b Batch) error {
	if len(b.Spans) == 0 && len(b.Contents) == 0 && len(b.Findings) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	runIDs := make([]string, 0, 1)
	seen := make(map[string]bool)
	for _, sp := range b.Spans {
		if !seen[sp.RunID] {
			seen[sp.RunID] = true
			runIDs = append(runIDs, sp.RunID)
		}
	}
	for _, f := range b.Findings {
		if !seen[f.RunID] {
			seen[f.RunID] = true
			runIDs = append(runIDs, f.RunID)
		}
	}
	for _, id := range runIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO runs (id, source) VALUES (?, ?) ON CONFLICT (id) DO NOTHING`,
			id, b.Source); err != nil {
			return fmt.Errorf("insert run %s: %w", id, err)
		}
	}
	for _, sp := range b.Spans {
		attrs, err := json.Marshal(sp.Attrs)
		if err != nil {
			return fmt.Errorf("marshal attrs of span %s: %w", sp.ID, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR REPLACE INTO spans
			 (id, run_id, parent_id, kind, name, started_at, ended_at,
			  tokens_in, tokens_out, cost_usd, status, attrs_json)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			sp.ID, sp.RunID, nullString(sp.ParentID), string(sp.Kind), sp.Name,
			nullNanos(sp.StartedAt), nullNanos(sp.EndedAt),
			sp.TokensIn, sp.TokensOut, sp.CostUSD, sp.Status, string(attrs)); err != nil {
			return fmt.Errorf("insert span %s: %w", sp.ID, err)
		}
	}
	for _, c := range b.Contents {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR REPLACE INTO contents (span_id, role, seq, body, media_type)
			 VALUES (?, ?, ?, ?, ?)`,
			c.SpanID, c.Role, c.Seq, c.Body, c.MediaType); err != nil {
			return fmt.Errorf("insert content %s/%d: %w", c.SpanID, c.Seq, err)
		}
	}
	for _, f := range b.Findings {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO findings (run_id, span_id, type, severity, detail_json)
			 VALUES (?, ?, ?, ?, ?)`,
			f.RunID, nullString(f.SpanID), f.Type, f.Severity, f.Detail); err != nil {
			return fmt.Errorf("insert finding %s/%s: %w", f.RunID, f.Type, err)
		}
	}
	for _, id := range runIDs {
		if _, err := tx.ExecContext(ctx, refreshRunSQL, id); err != nil {
			return fmt.Errorf("refresh run %s: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	s.notify()
	return nil
}

const refreshRunSQL = `
UPDATE runs SET
  started_at = (SELECT MIN(started_at) FROM spans WHERE run_id = runs.id),
  ended_at   = (SELECT MAX(ended_at) FROM spans WHERE run_id = runs.id),
  tokens_in  = (SELECT COALESCE(SUM(tokens_in), 0) FROM spans
                WHERE run_id = runs.id AND kind = 'llm'),
  tokens_out = (SELECT COALESCE(SUM(tokens_out), 0) FROM spans
                WHERE run_id = runs.id AND kind = 'llm'),
  model_main = COALESCE((SELECT json_extract(attrs_json, '$.model') FROM spans
                WHERE run_id = runs.id
                  AND json_extract(attrs_json, '$.model') IS NOT NULL
                GROUP BY json_extract(attrs_json, '$.model')
                ORDER BY COUNT(*) DESC, json_extract(attrs_json, '$.model')
                LIMIT 1), ''),
  cost_usd   = (SELECT SUM(cost_usd) FROM spans
                WHERE run_id = runs.id AND cost_usd IS NOT NULL),
  status     = CASE
                 -- The run's outcome is its root's outcome: real sessions
                 -- almost always contain some failed tool call.
                 WHEN EXISTS (SELECT 1 FROM spans
                              WHERE run_id = runs.id AND parent_id IS NULL
                                AND status = 'error') THEN 'error'
                 WHEN EXISTS (SELECT 1 FROM spans
                              WHERE run_id = runs.id AND parent_id IS NULL
                                AND ended_at IS NOT NULL) THEN 'ok'
                 ELSE 'running'
               END
WHERE id = ?`

// SetSpanCosts prices spans and refreshes their runs' cost totals.
func (s *Store) SetSpanCosts(ctx context.Context, costs map[string]float64) error {
	if len(costs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	runIDs := make(map[string]bool)
	for id, cost := range costs {
		var runID string
		if err := tx.QueryRowContext(ctx,
			`UPDATE spans SET cost_usd = ? WHERE id = ? RETURNING run_id`,
			cost, id).Scan(&runID); err != nil {
			return fmt.Errorf("price span %s: %w", id, err)
		}
		runIDs[runID] = true
	}
	for runID := range runIDs {
		if _, err := tx.ExecContext(ctx, refreshRunSQL, runID); err != nil {
			return fmt.Errorf("refresh run %s: %w", runID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	s.notify()
	return nil
}

// SetRunLabel names a run, creating the row first if no span arrived yet.
func (s *Store) SetRunLabel(ctx context.Context, runID, source, label string) error {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO runs (id, source, label) VALUES (?, ?, ?)
		 ON CONFLICT (id) DO UPDATE SET label = excluded.label`,
		runID, source, label); err != nil {
		return fmt.Errorf("label run %s: %w", runID, err)
	}
	s.notify()
	return nil
}

// PutLLMCache records model responses for replay, replacing any earlier entry
// for the same span.
func (s *Store) PutLLMCache(ctx context.Context, entries []CachedLLM) error {
	if len(entries) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, e := range entries {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR REPLACE INTO llm_cache (run_id, span_id, request_hash, response_json)
			 VALUES (?, ?, ?, ?)`,
			e.RunID, e.SpanID, e.RequestHash, e.Response); err != nil {
			return fmt.Errorf("cache llm span %s: %w", e.SpanID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// PutTaints replaces a run's taint edges with the given set, so a re-analysis
// whose findings changed leaves no stale propagation behind.
func (s *Store) PutTaints(ctx context.Context, runID string, edges []Taint) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM taints WHERE run_id = ?`, runID); err != nil {
		return fmt.Errorf("clear taints of %s: %w", runID, err)
	}
	for _, e := range edges {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO taints (run_id, span_id, source_span_id) VALUES (?, ?, ?)`,
			e.RunID, e.SpanID, e.SourceSpanID); err != nil {
			return fmt.Errorf("taint %s<-%s: %w", e.SpanID, e.SourceSpanID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// SetRunParent links a replay to the run it was replayed from, creating the
// row first so the link survives the child's spans arriving later.
func (s *Store) SetRunParent(ctx context.Context, runID, source, parentRunID string) error {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO runs (id, source, parent_run_id) VALUES (?, ?, ?)
		 ON CONFLICT (id) DO UPDATE SET parent_run_id = excluded.parent_run_id`,
		runID, source, parentRunID); err != nil {
		return fmt.Errorf("link run %s to %s: %w", runID, parentRunID, err)
	}
	s.notify()
	return nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullNanos(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UnixNano()
}
