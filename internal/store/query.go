package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// ListRuns returns all runs, most recently started first.
func (s *Store) ListRuns(ctx context.Context) ([]Run, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.id, r.source, r.started_at, r.ended_at, r.model_main, r.tokens_in,
		        r.tokens_out, r.cost_usd, r.status, r.label, r.parent_run_id,
		        (SELECT COUNT(*) FROM findings f WHERE f.run_id = r.id)
		 FROM runs r ORDER BY r.started_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()
	var runs []Run
	for rows.Next() {
		var r Run
		var started, ended sql.NullInt64
		var cost sql.NullFloat64
		var parent sql.NullString
		if err := rows.Scan(&r.ID, &r.Source, &started, &ended, &r.ModelMain,
			&r.TokensIn, &r.TokensOut, &cost, &r.Status, &r.Label, &parent,
			&r.Findings); err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		r.ParentRunID = parent.String
		r.StartedAt = fromNanos(started)
		r.EndedAt = fromNanos(ended)
		r.CostUSD = fromFloat(cost)
		runs = append(runs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	return runs, nil
}

// Spans returns a run's spans ordered by start time.
func (s *Store) Spans(ctx context.Context, runID string) ([]Span, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, run_id, parent_id, kind, name, started_at, ended_at,
		        tokens_in, tokens_out, cost_usd, status, attrs_json
		 FROM spans WHERE run_id = ? ORDER BY started_at, id`, runID)
	if err != nil {
		return nil, fmt.Errorf("list spans of %s: %w", runID, err)
	}
	defer rows.Close()
	spans, err := scanSpans(rows)
	if err != nil {
		return nil, fmt.Errorf("list spans of %s: %w", runID, err)
	}
	return spans, nil
}

// AllSpans returns every span in the store, for whole-corpus diagnostics.
func (s *Store) AllSpans(ctx context.Context) ([]Span, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, run_id, parent_id, kind, name, started_at, ended_at,
		        tokens_in, tokens_out, cost_usd, status, attrs_json
		 FROM spans ORDER BY started_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list spans: %w", err)
	}
	defer rows.Close()
	spans, err := scanSpans(rows)
	if err != nil {
		return nil, fmt.Errorf("list spans: %w", err)
	}
	return spans, nil
}

func scanSpans(rows *sql.Rows) ([]Span, error) {
	var spans []Span
	for rows.Next() {
		var sp Span
		var parent sql.NullString
		var kind string
		var started, ended sql.NullInt64
		var cost sql.NullFloat64
		var attrs string
		if err := rows.Scan(&sp.ID, &sp.RunID, &parent, &kind, &sp.Name, &started, &ended,
			&sp.TokensIn, &sp.TokensOut, &cost, &sp.Status, &attrs); err != nil {
			return nil, fmt.Errorf("scan span: %w", err)
		}
		sp.ParentID = parent.String
		sp.Kind = Kind(kind)
		sp.StartedAt = fromNanos(started)
		sp.EndedAt = fromNanos(ended)
		sp.CostUSD = fromFloat(cost)
		if err := json.Unmarshal([]byte(attrs), &sp.Attrs); err != nil {
			return nil, fmt.Errorf("unmarshal attrs of span %s: %w", sp.ID, err)
		}
		spans = append(spans, sp)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return spans, nil
}

// Contents returns a span's recorded contents in sequence order.
func (s *Store) Contents(ctx context.Context, spanID string) ([]Content, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT span_id, role, seq, body, media_type
		 FROM contents WHERE span_id = ? ORDER BY seq`, spanID)
	if err != nil {
		return nil, fmt.Errorf("list contents of %s: %w", spanID, err)
	}
	defer rows.Close()
	var contents []Content
	for rows.Next() {
		var c Content
		if err := rows.Scan(&c.SpanID, &c.Role, &c.Seq, &c.Body, &c.MediaType); err != nil {
			return nil, fmt.Errorf("scan content: %w", err)
		}
		contents = append(contents, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list contents of %s: %w", spanID, err)
	}
	return contents, nil
}

// Findings returns a run's findings in insertion order.
func (s *Store) Findings(ctx context.Context, runID string) ([]Finding, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, run_id, span_id, type, severity, detail_json
		 FROM findings WHERE run_id = ? ORDER BY id`, runID)
	if err != nil {
		return nil, fmt.Errorf("list findings of %s: %w", runID, err)
	}
	defer rows.Close()
	var findings []Finding
	for rows.Next() {
		var f Finding
		var spanID sql.NullString
		if err := rows.Scan(&f.ID, &f.RunID, &spanID, &f.Type, &f.Severity, &f.Detail); err != nil {
			return nil, fmt.Errorf("scan finding: %w", err)
		}
		f.SpanID = spanID.String
		findings = append(findings, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list findings of %s: %w", runID, err)
	}
	return findings, nil
}

// AllFindings returns every finding in the store, oldest first, for reporting
// across a whole ingested corpus.
func (s *Store) AllFindings(ctx context.Context) ([]Finding, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, run_id, span_id, type, severity, detail_json
		 FROM findings ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list findings: %w", err)
	}
	defer rows.Close()
	var findings []Finding
	for rows.Next() {
		var f Finding
		var spanID sql.NullString
		if err := rows.Scan(&f.ID, &f.RunID, &spanID, &f.Type, &f.Severity, &f.Detail); err != nil {
			return nil, fmt.Errorf("scan finding: %w", err)
		}
		f.SpanID = spanID.String
		findings = append(findings, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list findings: %w", err)
	}
	return findings, nil
}

// Taints returns a run's taint edges.
func (s *Store) Taints(ctx context.Context, runID string) ([]Taint, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT run_id, span_id, source_span_id FROM taints WHERE run_id = ?`, runID)
	if err != nil {
		return nil, fmt.Errorf("taints of %s: %w", runID, err)
	}
	defer rows.Close()
	var taints []Taint
	for rows.Next() {
		var t Taint
		if err := rows.Scan(&t.RunID, &t.SpanID, &t.SourceSpanID); err != nil {
			return nil, fmt.Errorf("scan taint: %w", err)
		}
		taints = append(taints, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("taints of %s: %w", runID, err)
	}
	return taints, nil
}

// ContentsForRun returns every content row of a run, grouped by span.
func (s *Store) ContentsForRun(ctx context.Context, runID string) (map[string][]Content, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.span_id, c.role, c.seq, c.body, c.media_type
		 FROM contents c JOIN spans s ON s.id = c.span_id
		 WHERE s.run_id = ? ORDER BY c.span_id, c.seq`, runID)
	if err != nil {
		return nil, fmt.Errorf("contents of run %s: %w", runID, err)
	}
	defer rows.Close()
	contents := make(map[string][]Content)
	for rows.Next() {
		var c Content
		if err := rows.Scan(&c.SpanID, &c.Role, &c.Seq, &c.Body, &c.MediaType); err != nil {
			return nil, fmt.Errorf("scan content: %w", err)
		}
		contents[c.SpanID] = append(contents[c.SpanID], c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("contents of run %s: %w", runID, err)
	}
	return contents, nil
}

// ResolveRunID expands a unique run id prefix to the full id.
func (s *Store) ResolveRunID(ctx context.Context, prefix string) (string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM runs WHERE id LIKE ? || '%' ORDER BY id LIMIT 3`, prefix)
	if err != nil {
		return "", fmt.Errorf("resolve run %q: %w", prefix, err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", fmt.Errorf("resolve run %q: %w", prefix, err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("resolve run %q: %w", prefix, err)
	}
	switch len(ids) {
	case 0:
		return "", fmt.Errorf("no run matches %q", prefix)
	case 1:
		return ids[0], nil
	}
	return "", fmt.Errorf("run id %q is ambiguous", prefix)
}

// ToolCall is one completed tool invocation in run order. Recorded separates a
// call whose arguments were captured from one whose were not, which a caller
// comparing inputs has to know: -no-content and instrumentors that skip
// arguments both leave Input empty without the calls being alike.
type ToolCall struct {
	SpanID   string
	Tool     string
	Input    string
	Recorded bool
}

// ToolCalls returns a run's completed tool calls with their inputs, in order.
func (s *Store) ToolCalls(ctx context.Context, runID string) ([]ToolCall, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT s.id,
		        COALESCE(json_extract(s.attrs_json, '$.tool_name'), s.name),
		        (SELECT c.body FROM contents c
		         WHERE c.span_id = s.id AND c.role = 'input'
		         ORDER BY c.seq LIMIT 1)
		 FROM spans s
		 WHERE s.run_id = ? AND s.kind = 'tool' AND s.ended_at IS NOT NULL
		 ORDER BY s.ended_at, s.id`, runID)
	if err != nil {
		return nil, fmt.Errorf("tool calls of %s: %w", runID, err)
	}
	defer rows.Close()
	var calls []ToolCall
	for rows.Next() {
		var c ToolCall
		var input sql.NullString
		if err := rows.Scan(&c.SpanID, &c.Tool, &input); err != nil {
			return nil, fmt.Errorf("scan tool call: %w", err)
		}
		c.Input, c.Recorded = input.String, input.Valid
		calls = append(calls, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tool calls of %s: %w", runID, err)
	}
	return calls, nil
}

// LLMCache returns a run's cached model responses in span order.
func (s *Store) LLMCache(ctx context.Context, runID string) ([]CachedLLM, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.run_id, c.span_id, c.request_hash, c.response_json
		 FROM llm_cache c JOIN spans s ON s.id = c.span_id
		 WHERE c.run_id = ? ORDER BY s.started_at, s.id`, runID)
	if err != nil {
		return nil, fmt.Errorf("llm cache of %s: %w", runID, err)
	}
	defer rows.Close()
	var cached []CachedLLM
	for rows.Next() {
		var c CachedLLM
		if err := rows.Scan(&c.RunID, &c.SpanID, &c.RequestHash, &c.Response); err != nil {
			return nil, fmt.Errorf("scan llm cache: %w", err)
		}
		cached = append(cached, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("llm cache of %s: %w", runID, err)
	}
	return cached, nil
}

// ContentStats sums recorded body sizes per span and role for one run.
func (s *Store) ContentStats(ctx context.Context, runID string) (map[string]map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.span_id, c.role, SUM(LENGTH(c.body))
		 FROM contents c JOIN spans s ON s.id = c.span_id
		 WHERE s.run_id = ?
		 GROUP BY c.span_id, c.role`, runID)
	if err != nil {
		return nil, fmt.Errorf("content stats of %s: %w", runID, err)
	}
	defer rows.Close()
	stats := make(map[string]map[string]int64)
	for rows.Next() {
		var spanID, role string
		var size int64
		if err := rows.Scan(&spanID, &role, &size); err != nil {
			return nil, fmt.Errorf("scan content stats: %w", err)
		}
		if stats[spanID] == nil {
			stats[spanID] = make(map[string]int64)
		}
		stats[spanID][role] = size
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("content stats of %s: %w", runID, err)
	}
	return stats, nil
}

// LatestResourceSamples returns the most recent reading per span of a run.
func (s *Store) LatestResourceSamples(ctx context.Context, runID string) (map[string]ResourceSample, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT span_id, MAX(ts), cpu_util, rss_bytes
		 FROM resource_samples WHERE run_id = ? GROUP BY span_id`, runID)
	if err != nil {
		return nil, fmt.Errorf("samples of %s: %w", runID, err)
	}
	defer rows.Close()
	latest := make(map[string]ResourceSample)
	for rows.Next() {
		var sm ResourceSample
		var ts sql.NullInt64
		var cpu sql.NullFloat64
		var rss sql.NullInt64
		if err := rows.Scan(&sm.SpanID, &ts, &cpu, &rss); err != nil {
			return nil, fmt.Errorf("scan sample: %w", err)
		}
		sm.RunID, sm.At, sm.CPUUtil = runID, fromNanos(ts), fromFloat(cpu)
		if rss.Valid {
			sm.RSSBytes = &rss.Int64
		}
		latest[sm.SpanID] = sm
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("samples of %s: %w", runID, err)
	}
	return latest, nil
}

// RunsWithOpenSamples returns runs that resource sampling saw executing a span
// the spans table never closed. Cheap enough to poll: the sample tables are
// empty unless a run opted into metrics.
func (s *Store) RunsWithOpenSamples(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT rs.run_id
		 FROM resource_samples rs
		 LEFT JOIN spans sp ON sp.id = rs.span_id
		 WHERE sp.ended_at IS NULL
		 ORDER BY rs.run_id`)
	if err != nil {
		return nil, fmt.Errorf("runs with open samples: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan run id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runs with open samples: %w", err)
	}
	return ids, nil
}

// SampledSpan pairs a span that resource sampling saw with the last reading
// taken under it. Known says the spans table has the row at all - false is the
// ordinary case for a crash, since a span is only exported once it ends.
type SampledSpan struct {
	SpanID     string
	Name       string
	ParentID   string
	LastSample time.Time
	Known      bool
	Ended      bool
}

// SampledSpans returns every span of a run that resource sampling observed,
// oldest reading first. The left join is deliberate: the span a crash
// interrupted is exactly the one with no row in spans.
func (s *Store) SampledSpans(ctx context.Context, runID string) ([]SampledSpan, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT rs.span_id, MAX(rs.ts), sp.id, sp.name, sp.parent_id, sp.ended_at
		 FROM resource_samples rs
		 LEFT JOIN spans sp ON sp.id = rs.span_id
		 WHERE rs.run_id = ?
		 GROUP BY rs.span_id
		 ORDER BY MAX(rs.ts), rs.span_id`, runID)
	if err != nil {
		return nil, fmt.Errorf("sampled spans of %s: %w", runID, err)
	}
	defer rows.Close()
	var out []SampledSpan
	for rows.Next() {
		var sp SampledSpan
		var ts, ended sql.NullInt64
		var known, name, parent sql.NullString
		if err := rows.Scan(&sp.SpanID, &ts, &known, &name, &parent, &ended); err != nil {
			return nil, fmt.Errorf("scan sampled span: %w", err)
		}
		sp.LastSample, sp.Name, sp.ParentID = fromNanos(ts), name.String, parent.String
		sp.Known, sp.Ended = known.Valid, ended.Valid
		out = append(out, sp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sampled spans of %s: %w", runID, err)
	}
	return out, nil
}

func fromNanos(v sql.NullInt64) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return time.Unix(0, v.Int64).UTC()
}

func fromFloat(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	return &v.Float64
}
