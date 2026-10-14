package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// PruneOptions configures which runs to remove from the store.
type PruneOptions struct {
	Keep      int
	OlderThan time.Duration
	CleanOK   bool
	DryRun    bool
}

// Vacuum executes SQLite VACUUM to reclaim storage.
func (s *Store) Vacuum(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, "VACUUM"); err != nil {
		return fmt.Errorf("vacuum db: %w", err)
	}
	return nil
}

// Prune removes matching runs and their child records, returning the count of runs deleted.
func (s *Store) Prune(ctx context.Context, opts PruneOptions) (int, error) {
	candidates, err := s.pruneCandidates(ctx, opts)
	if err != nil {
		return 0, err
	}
	if len(candidates) == 0 || opts.DryRun {
		return len(candidates), nil
	}
	if err := s.deleteRuns(ctx, candidates); err != nil {
		return 0, err
	}
	s.notify()
	return len(candidates), nil
}

func (s *Store) pruneCandidates(ctx context.Context, opts PruneOptions) ([]string, error) {
	var conds []string
	var args []any
	if opts.CleanOK {
		conds = append(conds, "status = 'ok'")
	}
	if opts.OlderThan > 0 {
		cutoff := time.Now().UTC().Add(-opts.OlderThan).UnixNano()
		conds = append(conds, "started_at < ?")
		args = append(args, cutoff)
	}
	query := "SELECT id FROM runs"
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += " ORDER BY started_at DESC"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query prune candidates: %w", err)
	}
	defer rows.Close()
	var all []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan run id: %w", err)
		}
		all = append(all, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("prune candidate rows: %w", err)
	}
	if opts.Keep > 0 {
		if len(all) <= opts.Keep {
			return nil, nil
		}
		return all[opts.Keep:], nil
	}
	return all, nil
}

func (s *Store) deleteRuns(ctx context.Context, runIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin prune tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	const chunkSize = 200
	for i := 0; i < len(runIDs); i += chunkSize {
		end := min(i+chunkSize, len(runIDs))
		chunk := runIDs[i:end]
		placeholders := strings.Repeat("?,", len(chunk))
		placeholders = placeholders[:len(placeholders)-1]
		args := make([]any, len(chunk))
		for j, id := range chunk {
			args[j] = id
		}
		queries := []string{
			fmt.Sprintf("DELETE FROM contents WHERE span_id IN (SELECT id FROM spans WHERE run_id IN (%s))", placeholders),
			fmt.Sprintf("DELETE FROM taints WHERE run_id IN (%s)", placeholders),
			fmt.Sprintf("DELETE FROM findings WHERE run_id IN (%s)", placeholders),
			fmt.Sprintf("DELETE FROM llm_cache WHERE run_id IN (%s)", placeholders),
			fmt.Sprintf("DELETE FROM resource_samples WHERE run_id IN (%s)", placeholders),
			fmt.Sprintf("DELETE FROM spans WHERE run_id IN (%s)", placeholders),
			fmt.Sprintf("DELETE FROM runs WHERE id IN (%s)", placeholders),
		}
		for _, q := range queries {
			if _, err := tx.ExecContext(ctx, q, args...); err != nil {
				return fmt.Errorf("execute %s: %w", q, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit prune tx: %w", err)
	}
	return nil
}

