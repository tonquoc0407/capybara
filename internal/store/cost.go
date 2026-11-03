package store

import (
	"context"
	"database/sql"
)

// UsageSummary separates estimated, reported, and unattributed project costs.
type UsageSummary struct {
	Project          string  `json:"project"`
	Runs             int64   `json:"runs"`
	TokensIn         int64   `json:"tokens_in"`
	TokensOut        int64   `json:"tokens_out"`
	EstimatedUSD     float64 `json:"estimated_usd"`
	ProviderUSD      float64 `json:"provider_usd"`
	UnattributedUSD  float64 `json:"unattributed_usd"`
	UnpricedLLMSpans int64   `json:"unpriced_llm_spans"`
}

// UsageByProject totals recorded usage without assigning a price to unpriced spans.
func (s *Store) UsageByProject(ctx context.Context) ([]UsageSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.project, COUNT(DISTINCT r.id),
		COALESCE(SUM(CASE WHEN sp.kind = 'llm' THEN sp.tokens_in ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN sp.kind = 'llm' THEN sp.tokens_out ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN p.kind = 'estimate' THEN sp.cost_usd ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN p.kind = 'provider' THEN sp.cost_usd ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN p.span_id IS NULL THEN sp.cost_usd ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN sp.kind = 'llm' AND sp.cost_usd IS NULL THEN 1 ELSE 0 END), 0)
		FROM runs r LEFT JOIN spans sp ON sp.run_id = r.id
		LEFT JOIN span_cost_provenance p ON p.span_id = sp.id
		GROUP BY r.project ORDER BY r.project`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UsageSummary{}
	for rows.Next() {
		var u UsageSummary
		if err := rows.Scan(&u.Project, &u.Runs, &u.TokensIn, &u.TokensOut,
			&u.EstimatedUSD, &u.ProviderUSD, &u.UnattributedUSD,
			&u.UnpricedLLMSpans); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// CostProvenanceForRun returns the source and pricing basis for each priced span.
func (s *Store) CostProvenanceForRun(ctx context.Context, runID string) ([]CostProvenance, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT p.span_id, p.kind, p.price_source, p.price_version,
		p.price_effective_at, p.checked_at, p.usage_source
		FROM span_cost_provenance p JOIN spans sp ON sp.id = p.span_id
		WHERE sp.run_id = ? ORDER BY sp.started_at, sp.id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CostProvenance{}
	for rows.Next() {
		var p CostProvenance
		var effective, checked sql.NullInt64
		if err := rows.Scan(&p.SpanID, &p.Kind, &p.PriceSource, &p.PriceVersion,
			&effective, &checked, &p.UsageSource); err != nil {
			return nil, err
		}
		if effective.Valid {
			at := fromNanos(effective)
			p.PriceEffectiveAt = &at
		}
		p.CheckedAt = fromNanos(checked)
		out = append(out, p)
	}
	return out, rows.Err()
}
