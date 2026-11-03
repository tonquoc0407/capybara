package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"
)

// Batch is one transactional set of spans, contents and findings from a single source.
type Batch struct {
	Source      string
	Spans       []Span
	Contents    []Content
	Findings    []Finding
	RunContexts map[string]RunContext
	// HubProject enforces ownership for authenticated ingestion. Empty means
	// local mode, which keeps the existing unrestricted behavior.
	HubProject string
	// DropContent removes bodies and untrusted raw attributes before persistence.
	// It also clears content previously stored for a re-imported span.
	DropContent bool
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
	incomingSpans := make(map[string]bool, len(b.Spans))
	for _, sp := range b.Spans {
		incomingSpans[sp.ID] = true
		if !seen[sp.RunID] {
			seen[sp.RunID] = true
			runIDs = append(runIDs, sp.RunID)
		}
	}
	contentOnly := make(map[string]bool)
	for _, c := range b.Contents {
		if incomingSpans[c.SpanID] || contentOnly[c.SpanID] {
			continue
		}
		var runID string
		if err := tx.QueryRowContext(ctx, `SELECT run_id FROM spans WHERE id = ?`, c.SpanID).Scan(&runID); err != nil {
			return fmt.Errorf("content span %s: %w", c.SpanID, err)
		}
		contentOnly[c.SpanID] = true
		if !seen[runID] {
			seen[runID] = true
			runIDs = append(runIDs, runID)
		}
	}
	for _, f := range b.Findings {
		if !seen[f.RunID] {
			seen[f.RunID] = true
			runIDs = append(runIDs, f.RunID)
		}
	}
	for _, id := range runIDs {
		project := "default"
		if b.HubProject != "" {
			project = b.HubProject
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO runs (id, source, project) VALUES (?, ?, ?) ON CONFLICT (id) DO NOTHING`,
			id, b.Source, project); err != nil {
			return fmt.Errorf("insert run %s: %w", id, err)
		}
		if b.HubProject != "" {
			var owner string
			if err := tx.QueryRowContext(ctx, `SELECT project FROM runs WHERE id = ?`, id).Scan(&owner); err != nil {
				return err
			}
			if owner != b.HubProject {
				return fmt.Errorf("run %s belongs to another project", id)
			}
		}
		meta := b.RunContexts[id]
		if b.HubProject != "" {
			meta.Project = b.HubProject
		}
		if _, err := tx.ExecContext(ctx, `UPDATE runs SET
			project = CASE WHEN project = 'default' AND ? != '' THEN ? ELSE project END,
			environment = CASE WHEN environment = '' THEN ? ELSE environment END,
			release = CASE WHEN release = '' THEN ? ELSE release END,
			session_id = CASE WHEN session_id = '' THEN ? ELSE session_id END,
			user_id = CASE WHEN user_id = '' THEN ? ELSE user_id END WHERE id = ?`,
			meta.Project, meta.Project, meta.Environment, meta.Release, meta.SessionID, meta.UserID, id); err != nil {
			return fmt.Errorf("context of run %s: %w", id, err)
		}
	}
	for _, sp := range b.Spans {
		if sp.CostUSD != nil && (math.IsNaN(*sp.CostUSD) || math.IsInf(*sp.CostUSD, 0) || *sp.CostUSD < 0) {
			return fmt.Errorf("invalid cost for span %s", sp.ID)
		}
		var oldRun string
		var oldTokensIn, oldTokensOut int64
		var oldAttrs string
		err := tx.QueryRowContext(ctx, `SELECT run_id, tokens_in, tokens_out, attrs_json FROM spans WHERE id = ?`, sp.ID).
			Scan(&oldRun, &oldTokensIn, &oldTokensOut, &oldAttrs)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && oldRun != sp.RunID {
			return fmt.Errorf("span %s belongs to another run", sp.ID)
		}
		if b.DropContent {
			sp.Attrs.Raw = metadataOnly(sp.Attrs.Raw)
			if _, err := tx.ExecContext(ctx, `DELETE FROM contents WHERE span_id = ?`, sp.ID); err != nil {
				return fmt.Errorf(`clear content of span %s: %w`, sp.ID, err)
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM llm_cache WHERE span_id = ?`, sp.ID); err != nil {
				return fmt.Errorf(`clear llm cache of span %s: %w`, sp.ID, err)
			}
		}
		attrs, err := json.Marshal(sp.Attrs)
		if err != nil {
			return fmt.Errorf("marshal attrs of span %s: %w", sp.ID, err)
		}
		// A repeated import with changed pricing inputs must not retain a
		// previous automatic estimate. Provider amounts are left untouched.
		if oldRun != "" && sp.CostUSD == nil &&
			(oldTokensIn != sp.TokensIn || oldTokensOut != sp.TokensOut || oldAttrs != string(attrs)) {
			if _, err := tx.ExecContext(ctx, `UPDATE spans SET cost_usd = NULL WHERE id = ? AND EXISTS
				(SELECT 1 FROM span_cost_provenance WHERE span_id = ? AND kind = 'estimate')`, sp.ID, sp.ID); err != nil {
				return fmt.Errorf("clear stale estimate of span %s: %w", sp.ID, err)
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM span_cost_provenance WHERE span_id = ? AND kind = 'estimate'`, sp.ID); err != nil {
				return fmt.Errorf("clear stale provenance of span %s: %w", sp.ID, err)
			}
		}
		// The incoming amount is explicit. An earlier estimate's provenance
		// cannot describe it; keep provider provenance if one exists.
		if oldRun != "" && sp.CostUSD != nil {
			if _, err := tx.ExecContext(ctx, `DELETE FROM span_cost_provenance WHERE span_id = ? AND kind = 'estimate'`, sp.ID); err != nil {
				return fmt.Errorf("clear replaced estimate provenance of span %s: %w", sp.ID, err)
			}
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO spans
			 (id, run_id, parent_id, kind, name, started_at, ended_at,
			  tokens_in, tokens_out, cost_usd, status, attrs_json)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET
			 parent_id = COALESCE(excluded.parent_id, spans.parent_id),
			 kind = excluded.kind, name = excluded.name,
			 started_at = COALESCE(excluded.started_at, spans.started_at),
			 ended_at = COALESCE(excluded.ended_at, spans.ended_at),
			 tokens_in = excluded.tokens_in, tokens_out = excluded.tokens_out,
			 cost_usd = COALESCE(excluded.cost_usd, spans.cost_usd),
			 status = excluded.status, attrs_json = excluded.attrs_json,
			 analyzed = 0, revision = spans.revision + 1`,
			sp.ID, sp.RunID, nullString(sp.ParentID), string(sp.Kind), sp.Name,
			nullNanos(sp.StartedAt), nullNanos(sp.EndedAt),
			sp.TokensIn, sp.TokensOut, sp.CostUSD, sp.Status, string(attrs)); err != nil {
			return fmt.Errorf("insert span %s: %w", sp.ID, err)
		}
	}
	for spanID := range contentOnly {
		if _, err := tx.ExecContext(ctx, `UPDATE spans SET analyzed = 0, revision = revision + 1 WHERE id = ?`, spanID); err != nil {
			return err
		}
	}
	for _, c := range b.Contents {
		if b.DropContent {
			break
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR REPLACE INTO contents (span_id, role, seq, body, media_type)
			 VALUES (?, ?, ?, ?, ?)`,
			c.SpanID, c.Role, c.Seq, c.Body, c.MediaType); err != nil {
			return fmt.Errorf("insert content %s/%d: %w", c.SpanID, c.Seq, err)
		}
	}
	for _, f := range b.Findings {
		if b.DropContent {
			f.Detail = `{}`
		}
		if f.Origin == "" {
			f.Origin = "import"
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO findings (run_id, span_id, type, severity, detail_json, origin)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			f.RunID, nullString(f.SpanID), f.Type, f.Severity, f.Detail, f.Origin); err != nil {
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
                 -- Root failures are conclusive; failed tool calls alone are
                 -- common in otherwise successful agent runs.
                 WHEN EXISTS (SELECT 1 FROM spans
                              WHERE run_id = runs.id AND parent_id IS NULL
                                AND status = 'error') THEN 'error'
                 -- A generic successful wrapper can hide a terminal model
                 -- failure (and some SDKs fail to mark their agent span).
                 -- Later successful model turns count as recovery.
                 WHEN EXISTS (SELECT 1 FROM spans s
                              WHERE s.run_id = runs.id AND s.kind = 'llm'
                                AND s.status = 'error' AND s.ended_at IS NOT NULL
                                AND NOT EXISTS (SELECT 1 FROM spans later
                                                WHERE later.run_id = s.run_id
                                                  AND later.kind = 'llm'
                                                  AND later.status = 'ok'
                                                  AND later.ended_at > s.ended_at))
                   THEN 'error'
                 -- Failed agent children can be followed by a successful
                 -- sibling agent; a successful ancestor is not recovery.
                 WHEN EXISTS (SELECT 1 FROM spans s
                              WHERE s.run_id = runs.id AND s.kind = 'agent'
                                AND s.status = 'error' AND s.ended_at IS NOT NULL
                                AND NOT EXISTS (SELECT 1 FROM spans later
                                                WHERE later.run_id = s.run_id
                                                  AND later.kind = 'agent'
                                                  AND later.status = 'ok'
                                                  AND later.parent_id IS s.parent_id
                                                  AND later.ended_at > s.ended_at))
                   THEN 'error'
                 WHEN EXISTS (SELECT 1 FROM spans
                              WHERE run_id = runs.id AND parent_id IS NULL
                                AND ended_at IS NOT NULL) THEN 'ok'
                 ELSE 'running'
               END
WHERE id = ?`

// SetSpanCosts prices spans and refreshes their runs' cost totals.
func (s *Store) SetSpanCosts(ctx context.Context, costs map[string]float64) error {
	estimates := make(map[string]SpanEstimate, len(costs))
	for id, cost := range costs {
		estimates[id] = SpanEstimate{CostUSD: cost, Provenance: CostProvenance{
			Kind: "estimate", PriceSource: "legacy", PriceVersion: "unknown",
			UsageSource: "observed", CheckedAt: time.Now().UTC(),
		}}
	}
	return s.SetSpanEstimates(ctx, estimates)
}

// SetSpanEstimates stores costs and their provenance in one transaction.
func (s *Store) SetSpanEstimates(ctx context.Context, estimates map[string]SpanEstimate) error {
	return s.setSpanEstimates(ctx, estimates, false)
}

// SetAutomaticSpanEstimates preserves explicit/provider amounts. Automatic
// prices can refresh their own estimates after late token or model updates.
func (s *Store) SetAutomaticSpanEstimates(ctx context.Context, estimates map[string]SpanEstimate) error {
	return s.setSpanEstimates(ctx, estimates, true)
}

func (s *Store) setSpanEstimates(ctx context.Context, estimates map[string]SpanEstimate, automatic bool) error {
	if len(estimates) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	runIDs := make(map[string]bool)
	for id, estimate := range estimates {
		if math.IsNaN(estimate.CostUSD) || math.IsInf(estimate.CostUSD, 0) || estimate.CostUSD < 0 {
			return fmt.Errorf("invalid cost for span %s", id)
		}
		p := estimate.Provenance
		if p.Kind == "" {
			p.Kind = "estimate"
		}
		if p.Kind != "estimate" && p.Kind != "provider" {
			return fmt.Errorf("invalid cost kind for span %s", id)
		}
		if p.PriceSource == "" {
			p.PriceSource = "unknown"
		}
		if p.PriceVersion == "" {
			p.PriceVersion = "unknown"
		}
		if p.UsageSource == "" {
			p.UsageSource = "unknown"
		}
		if p.CheckedAt.IsZero() {
			p.CheckedAt = time.Now().UTC()
		}
		var runID string
		query := `UPDATE spans SET cost_usd = ? WHERE id = ?`
		queryArgs := []any{estimate.CostUSD, id}
		if automatic {
			if estimate.Revision < 1 {
				return fmt.Errorf("automatic estimate for span %s needs a revision", id)
			}
			query += ` AND (cost_usd IS NULL OR EXISTS (SELECT 1 FROM span_cost_provenance p
				WHERE p.span_id = spans.id AND p.kind = 'estimate' AND p.price_source IN
				('capybara embedded pricing', 'capybara embedded + local override')))`
			query += ` AND revision = ?`
			queryArgs = append(queryArgs, estimate.Revision)
		}
		if err := tx.QueryRowContext(ctx, query+` RETURNING run_id`, queryArgs...).Scan(&runID); err != nil {
			if automatic && errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return fmt.Errorf("price span %s: %w", id, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO span_cost_provenance
			(span_id, kind, price_source, price_version, price_effective_at, checked_at, usage_source)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (span_id) DO UPDATE SET kind = excluded.kind,
			price_source = excluded.price_source, price_version = excluded.price_version,
			price_effective_at = excluded.price_effective_at, checked_at = excluded.checked_at,
			usage_source = excluded.usage_source`,
			id, p.Kind, p.PriceSource, p.PriceVersion, nullTimePtr(p.PriceEffectiveAt),
			p.CheckedAt.UnixNano(), p.UsageSource); err != nil {
			return fmt.Errorf("record cost provenance for span %s: %w", id, err)
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

// PutResourceSamples records process readings, creating the run row first: a
// sample can arrive before the first span of its trace has ended, and for a run
// that dies mid-call it may be the only row that ever lands.
func (s *Store) PutResourceSamples(ctx context.Context, source string, samples []ResourceSample) error {
	return s.putResourceSamples(ctx, source, "", samples)
}

func (s *Store) PutProjectResourceSamples(ctx context.Context, source, project string, samples []ResourceSample) error {
	if project == "" {
		return fmt.Errorf("project is required")
	}
	return s.putResourceSamples(ctx, source, project, samples)
}

func (s *Store) putResourceSamples(ctx context.Context, source, project string, samples []ResourceSample) error {
	if len(samples) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	seen := make(map[string]bool)
	for _, sm := range samples {
		if !seen[sm.RunID] {
			seen[sm.RunID] = true
			owner := project
			if owner == "" {
				owner = "default"
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO runs (id, source, project) VALUES (?, ?, ?) ON CONFLICT (id) DO NOTHING`,
				sm.RunID, source, owner); err != nil {
				return fmt.Errorf("insert run %s: %w", sm.RunID, err)
			}
			if project != "" {
				if err := tx.QueryRowContext(ctx, `SELECT project FROM runs WHERE id = ?`, sm.RunID).Scan(&owner); err != nil {
					return err
				}
				if owner != project {
					return fmt.Errorf("run %s belongs to another project", sm.RunID)
				}
			}
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR REPLACE INTO resource_samples
			 (run_id, span_id, ts, cpu_util, rss_bytes, gpu_util, gpu_mem_bytes, span_name)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			sm.RunID, sm.SpanID, sm.At.UnixNano(), sm.CPUUtil, sm.RSSBytes,
			sm.GPUUtil, sm.GPUMemBytes, nullString(sm.SpanName)); err != nil {
			return fmt.Errorf("insert sample %s@%d: %w", sm.SpanID, sm.At.UnixNano(), err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
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

func nullTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return nullNanos(*t)
}
