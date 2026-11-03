package store

import (
	"context"
	"errors"
	"fmt"
)

// ErrAnalysisStale means a span changed after the analyzer read its run.
// The changed span remains unanalyzed and a later sweep can retry.
var ErrAnalysisStale = errors.New("analysis snapshot is stale")

// ReplaceAnalysis atomically replaces derived findings and taints for a run.
// Imported findings and derived findings on unchanged spans are retained.
func (s *Store) ReplaceAnalysis(ctx context.Context, runID string, snapshot, fresh []Span, findings []Finding, taints []Taint) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	want := make(map[string]int64, len(snapshot))
	for _, sp := range snapshot {
		if sp.RunID != runID {
			return fmt.Errorf("snapshot span %s belongs to another run", sp.ID)
		}
		want[sp.ID] = sp.Revision
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, revision FROM spans WHERE run_id = ?`, runID)
	if err != nil {
		return err
	}
	seen := 0
	stale := false
	for rows.Next() {
		var id string
		var revision int64
		if err := rows.Scan(&id, &revision); err != nil {
			rows.Close()
			return err
		}
		seen++
		if want[id] != revision {
			stale = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if stale || seen != len(want) {
		return ErrAnalysisStale
	}
	for _, sp := range fresh {
		if sp.RunID != runID {
			return fmt.Errorf("fresh span %s belongs to another run", sp.ID)
		}
		if want[sp.ID] != sp.Revision {
			return ErrAnalysisStale
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM findings WHERE run_id = ? AND span_id = ? AND origin = 'analyzer_span'`, runID, sp.ID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM findings WHERE run_id = ? AND origin = 'analyzer_run'`, runID); err != nil {
		return err
	}
	for _, f := range findings {
		if f.RunID != runID || (f.Origin != "analyzer_span" && f.Origin != "analyzer_run") {
			return fmt.Errorf("invalid analysis finding for run %s", runID)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO findings (run_id, span_id, type, severity, detail_json, origin)
			 VALUES (?, ?, ?, ?, ?, ?)`, f.RunID, nullString(f.SpanID), f.Type, f.Severity, f.Detail, f.Origin); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM taints WHERE run_id = ?`, runID); err != nil {
		return err
	}
	for _, edge := range taints {
		if edge.RunID != runID {
			return fmt.Errorf("taint belongs to another run")
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO taints (run_id, span_id, source_span_id) VALUES (?, ?, ?)`,
			edge.RunID, edge.SpanID, edge.SourceSpanID); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.notify()
	return nil
}

// ReconcileOrphans replaces liveness findings, including ones that ceased to
// apply when a late span arrived. It returns runs whose taints need refresh.
func (s *Store) ReconcileOrphans(ctx context.Context, current []Finding) ([]string, error) {
	want := make(map[string]Finding, len(current))
	for _, f := range current {
		if f.Type != "orphaned_span" {
			return nil, fmt.Errorf("invalid orphan finding %s", f.Type)
		}
		want[f.RunID] = f
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx,
		`SELECT id, run_id, coalesce(span_id, ''), severity, detail_json
		 FROM findings WHERE origin = 'analyzer_orphan'`)
	if err != nil {
		return nil, err
	}
	type oldFinding struct {
		id                              int64
		runID, spanID, severity, detail string
	}
	var old []oldFinding
	for rows.Next() {
		var f oldFinding
		if err := rows.Scan(&f.id, &f.runID, &f.spanID, &f.severity, &f.detail); err != nil {
			rows.Close()
			return nil, err
		}
		old = append(old, f)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	changed := make(map[string]bool)
	kept := make(map[string]bool)
	for _, f := range old {
		next, ok := want[f.runID]
		if ok && !kept[f.runID] && f.spanID == next.SpanID && f.severity == next.Severity && f.detail == next.Detail {
			kept[f.runID] = true
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM findings WHERE id = ?`, f.id); err != nil {
			return nil, err
		}
		changed[f.runID] = true
	}
	for runID, f := range want {
		if kept[runID] {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO findings (run_id, span_id, type, severity, detail_json, origin)
			 VALUES (?, ?, 'orphaned_span', ?, ?, 'analyzer_orphan')`,
			runID, nullString(f.SpanID), f.Severity, f.Detail); err != nil {
			return nil, err
		}
		changed[runID] = true
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if len(changed) > 0 {
		s.notify()
	}
	runs := make([]string, 0, len(changed))
	for runID := range changed {
		runs = append(runs, runID)
	}
	return runs, nil
}
