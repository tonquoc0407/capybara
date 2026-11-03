package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// PromptVersion is a stored content snapshot identified by its SHA-256.
type PromptVersion struct {
	ID         string    `json:"id"`
	Project    string    `json:"project"`
	Name       string    `json:"name"`
	Version    int64     `json:"version"`
	SHA256     string    `json:"sha256"`
	Content    string    `json:"content"`
	SourcePath string    `json:"source_path"`
	CreatedAt  time.Time `json:"created_at"`
}

// PromptReference links a span hash to a registered prompt when one exists.
type PromptReference struct {
	SpanID string         `json:"span_id"`
	SHA256 string         `json:"sha256"`
	Prompt *PromptVersion `json:"prompt,omitempty"`
}

// RegisterPrompt records an explicitly selected local file. Re-registering
// identical content returns its existing version; content changes create one.
func (s *Store) RegisterPrompt(ctx context.Context, project, name, content, sourcePath string) (PromptVersion, error) {
	if project == "" {
		project = "default"
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return PromptVersion{}, errors.New("prompt name is required")
	}
	h := sha256.Sum256([]byte(content))
	sha := hex.EncodeToString(h[:])
	idSum := sha256.Sum256([]byte(project + "\x00" + name + "\x00" + sha))
	id := hex.EncodeToString(idSum[:])
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PromptVersion{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var p PromptVersion
	var at int64
	err = tx.QueryRowContext(ctx, `SELECT id, project, name, version, sha256, content, source_path, created_at
		FROM prompt_versions WHERE project = ? AND name = ? AND sha256 = ?`, project, name, sha).Scan(
		&p.ID, &p.Project, &p.Name, &p.Version, &p.SHA256, &p.Content, &p.SourcePath, &at)
	if err == nil {
		p.CreatedAt = time.Unix(0, at).UTC()
		return p, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return PromptVersion{}, err
	}
	var version int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM prompt_versions
		WHERE project = ? AND name = ?`, project, name).Scan(&version); err != nil {
		return PromptVersion{}, err
	}
	p = PromptVersion{
		ID: id, Project: project, Name: name, Version: version,
		SHA256: sha, Content: content, SourcePath: sourcePath, CreatedAt: time.Now().UTC(),
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO prompt_versions
		(id, project, name, version, sha256, content, source_path, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, p.ID, p.Project, p.Name, p.Version,
		p.SHA256, p.Content, p.SourcePath, p.CreatedAt.UnixNano()); err != nil {
		return PromptVersion{}, err
	}
	if err := tx.Commit(); err != nil {
		return PromptVersion{}, err
	}
	s.notify()
	return p, nil
}

// ListPromptVersions omits prompt bodies; PromptVersion loads one on demand.
func (s *Store) ListPromptVersions(ctx context.Context, project, name string) ([]PromptVersion, error) {
	query := `SELECT id, project, name, version, sha256, source_path, created_at FROM prompt_versions WHERE project = ?`
	args := []any{project}
	if name != "" {
		query += ` AND name = ?`
		args = append(args, name)
	}
	query += ` ORDER BY name, version DESC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PromptVersion{}
	for rows.Next() {
		var p PromptVersion
		var at int64
		if err := rows.Scan(&p.ID, &p.Project, &p.Name, &p.Version,
			&p.SHA256, &p.SourcePath, &at); err != nil {
			return nil, err
		}
		p.CreatedAt = time.Unix(0, at).UTC()
		out = append(out, p)
	}
	return out, rows.Err()
}

// PromptVersion returns the full content of a specific registered version.
func (s *Store) PromptVersion(ctx context.Context, id string) (PromptVersion, error) {
	var p PromptVersion
	var at int64
	err := s.db.QueryRowContext(ctx, `SELECT id, project, name, version, sha256, content, source_path, created_at
		FROM prompt_versions WHERE id = ?`, id).Scan(&p.ID, &p.Project, &p.Name,
		&p.Version, &p.SHA256, &p.Content, &p.SourcePath, &at)
	if err != nil {
		return PromptVersion{}, err
	}
	p.CreatedAt = time.Unix(0, at).UTC()
	return p, nil
}

// PromptReferencesForRun resolves capybara.prompt.sha256 attributes without
// pretending an unregistered hash is a known prompt version.
func (s *Store) PromptReferencesForRun(ctx context.Context, runID string) ([]PromptReference, error) {
	var project string
	if err := s.db.QueryRowContext(ctx, `SELECT project FROM runs WHERE id = ?`, runID).Scan(&project); err != nil {
		return nil, err
	}
	spans, err := s.Spans(ctx, runID)
	if err != nil {
		return nil, err
	}
	out := []PromptReference{}
	for _, sp := range spans {
		sha, ok := sp.Attrs.Raw["capybara.prompt.sha256"].(string)
		if !ok || sha == "" {
			continue
		}
		ref := PromptReference{SpanID: sp.ID, SHA256: sha}
		var p PromptVersion
		var at int64
		err := s.db.QueryRowContext(ctx, `SELECT id, project, name, version, sha256, content, source_path, created_at
			FROM prompt_versions WHERE project = ? AND sha256 = ? ORDER BY name LIMIT 1`, project, sha).Scan(
			&p.ID, &p.Project, &p.Name, &p.Version, &p.SHA256, &p.Content, &p.SourcePath, &at)
		if err == nil {
			p.CreatedAt = time.Unix(0, at).UTC()
			ref.Prompt = &p
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("resolve prompt hash %s: %w", sha, err)
		}
		out = append(out, ref)
	}
	return out, nil
}
