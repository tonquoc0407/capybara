package store

import (
	"context"
	"errors"
	"strings"
	"time"
)

// IncidentAnnotation records a human disposition or investigation note for a run.
type IncidentAnnotation struct {
	ID        int64     `json:"id"`
	RunID     string    `json:"run_id"`
	Project   string    `json:"project"`
	Status    string    `json:"status"`
	Note      string    `json:"note"`
	Author    string    `json:"author"`
	CreatedAt time.Time `json:"created_at"`
}

var validAnnotationStatuses = map[string]bool{
	"open": true, "investigating": true, "resolved": true, "ignored": true,
}

// AddIncidentAnnotation attaches one validated note to a run.
func (s *Store) AddIncidentAnnotation(ctx context.Context, runID, status, note, author string) (IncidentAnnotation, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	note = strings.TrimSpace(note)
	author = strings.TrimSpace(author)
	if !validAnnotationStatuses[status] {
		return IncidentAnnotation{}, errors.New("annotation status must be open, investigating, resolved or ignored")
	}
	if note == "" {
		return IncidentAnnotation{}, errors.New("annotation note is required")
	}
	if author == "" {
		author = "local"
	}
	var project string
	if err := s.db.QueryRowContext(ctx, `SELECT project FROM runs WHERE id = ?`, runID).Scan(&project); err != nil {
		return IncidentAnnotation{}, err
	}
	at := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `INSERT INTO incident_annotations(run_id, project, status, note, author, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, runID, project, status, note, author, at.UnixNano())
	if err != nil {
		return IncidentAnnotation{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return IncidentAnnotation{}, err
	}
	s.notify()
	return IncidentAnnotation{ID: id, RunID: runID, Project: project, Status: status, Note: note, Author: author, CreatedAt: at}, nil
}

// IncidentAnnotations returns annotations in chronological order.
func (s *Store) IncidentAnnotations(ctx context.Context, runID string) ([]IncidentAnnotation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, run_id, project, status, note, author, created_at
		FROM incident_annotations WHERE run_id = ? ORDER BY created_at, id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IncidentAnnotation
	for rows.Next() {
		var a IncidentAnnotation
		var at int64
		if err := rows.Scan(&a.ID, &a.RunID, &a.Project, &a.Status, &a.Note, &a.Author, &at); err != nil {
			return nil, err
		}
		a.CreatedAt = time.Unix(0, at).UTC()
		out = append(out, a)
	}
	return out, rows.Err()
}
