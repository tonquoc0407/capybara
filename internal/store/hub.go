package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

type HubToken struct {
	ID        string     `json:"id"`
	Project   string     `json:"project"`
	Role      string     `json:"role"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

type AuditEvent struct {
	ID           int64     `json:"id"`
	At           time.Time `json:"at"`
	ActorTokenID string    `json:"actor_token_id,omitempty"`
	Action       string    `json:"action"`
	Project      string    `json:"project"`
	Detail       string    `json:"detail,omitempty"`
}

// CreateHubToken prints the secret only to its caller. The database holds only
// its SHA-256 hash; high entropy makes offline guessing impractical.
func (s *Store) CreateHubToken(ctx context.Context, project, role string) (HubToken, string, error) {
	if project == "" {
		return HubToken{}, "", errors.New("project is required")
	}
	if role != "admin" && role != "editor" && role != "viewer" {
		return HubToken{}, "", errors.New("invalid role")
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return HubToken{}, "", err
	}
	secret := hex.EncodeToString(raw[:])
	hash := sha256.Sum256([]byte(secret))
	id := hex.EncodeToString(hash[:8])
	at := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return HubToken{}, "", err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO hub_tokens (id, token_hash, project, role, created_at)
		VALUES (?, ?, ?, ?, ?)`, id, hex.EncodeToString(hash[:]), project, role, at.UnixNano()); err != nil {
		return HubToken{}, "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events (at, actor_token_id, action, project, detail)
		VALUES (?, '', 'token.create', ?, ?)`, at.UnixNano(), project, id+":"+role); err != nil {
		return HubToken{}, "", err
	}
	if err := tx.Commit(); err != nil {
		return HubToken{}, "", err
	}
	return HubToken{ID: id, Project: project, Role: role, CreatedAt: at}, secret, nil
}

func (s *Store) AuthenticateHubToken(ctx context.Context, secret string) (HubToken, error) {
	if len(secret) != 64 {
		return HubToken{}, errors.New("invalid token")
	}
	hash := sha256.Sum256([]byte(secret))
	var t HubToken
	var at int64
	err := s.db.QueryRowContext(ctx, `SELECT id, project, role, created_at FROM hub_tokens
		WHERE token_hash = ? AND revoked_at IS NULL`, hex.EncodeToString(hash[:])).Scan(
		&t.ID, &t.Project, &t.Role, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return HubToken{}, errors.New("invalid token")
	}
	if err != nil {
		return HubToken{}, err
	}
	t.CreatedAt = time.Unix(0, at).UTC()
	return t, nil
}

func (s *Store) RevokeHubToken(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var project string
	if err := tx.QueryRowContext(ctx, `SELECT project FROM hub_tokens WHERE id = ? AND revoked_at IS NULL`, id).Scan(&project); err != nil {
		return err
	}
	at := time.Now().UTC().UnixNano()
	if _, err := tx.ExecContext(ctx, `UPDATE hub_tokens SET revoked_at = ? WHERE id = ?`, at, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events (at, actor_token_id, action, project, detail)
		VALUES (?, '', 'token.revoke', ?, ?)`, at, project, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListHubTokens(ctx context.Context, project string) ([]HubToken, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, project, role, created_at, revoked_at
		FROM hub_tokens WHERE project = ? ORDER BY created_at DESC`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HubToken{}
	for rows.Next() {
		var t HubToken
		var created int64
		var revoked sql.NullInt64
		if err := rows.Scan(&t.ID, &t.Project, &t.Role, &created, &revoked); err != nil {
			return nil, err
		}
		t.CreatedAt = time.Unix(0, created).UTC()
		if revoked.Valid {
			at := time.Unix(0, revoked.Int64).UTC()
			t.RevokedAt = &at
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) AuditEvents(ctx context.Context, project string, limit int) ([]AuditEvent, error) {
	if limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("limit must be 1-1000")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, at, actor_token_id, action, project, detail
		FROM audit_events WHERE project = ? OR project = '*' ORDER BY at DESC, id DESC LIMIT ?`, project, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEvent{}
	for rows.Next() {
		var e AuditEvent
		var at int64
		var actor sql.NullString
		if err := rows.Scan(&e.ID, &at, &actor, &e.Action, &e.Project, &e.Detail); err != nil {
			return nil, err
		}
		e.ActorTokenID = actor.String
		e.At = time.Unix(0, at).UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) RecordAudit(ctx context.Context, actorTokenID, action, project, detail string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_events (at, actor_token_id, action, project, detail)
		VALUES (?, ?, ?, ?, ?)`, time.Now().UTC().UnixNano(), actorTokenID, action, project, detail)
	return err
}
