package store

import "context"

// Diagnostics summarizes what arrived without reading prompt or tool bodies.
type Diagnostics struct {
	Runs         int64 `json:"runs"`
	Spans        int64 `json:"spans"`
	Logs         int64 `json:"logs"`
	Unmapped     int64 `json:"unmapped_spans"`
	MissingUsage int64 `json:"llm_spans_without_usage"`
	MissingCost  int64 `json:"llm_spans_without_cost"`
}

// Diagnostics counts stored runs, spans, logs, and missing usage or cost.
func (s *Store) Diagnostics(ctx context.Context) (Diagnostics, error) {
	var d Diagnostics
	err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM runs),
		(SELECT COUNT(*) FROM spans),
		(SELECT COUNT(*) FROM trace_logs),
		(SELECT COUNT(*) FROM spans WHERE kind = 'other'),
		(SELECT COUNT(*) FROM spans WHERE kind = 'llm' AND tokens_in = 0 AND tokens_out = 0),
		(SELECT COUNT(*) FROM spans WHERE kind = 'llm' AND cost_usd IS NULL)`).Scan(
		&d.Runs, &d.Spans, &d.Logs, &d.Unmapped, &d.MissingUsage, &d.MissingCost)
	return d, err
}
