package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"
)

// ErrEventConflict means an event ID was reused with a different payload.
var ErrEventConflict = errors.New("event id already used with different data")

// ErrSpanNotStarted means an update or end arrived before the span's start.
var ErrSpanNotStarted = errors.New("span has not started in this run")

// ErrEventTime means a span's end precedes its recorded start.
var ErrEventTime = errors.New("span end time precedes start time")

// ErrEventCost means a supplied cost is negative or nonfinite.
var ErrEventCost = errors.New("cost_usd must be a finite nonnegative number")

// SpanEvent is one incremental event sent by a local application.
type SpanEvent struct {
	EventID   string         `json:"event_id"`
	Type      string         `json:"type"`
	RunID     string         `json:"run_id"`
	SpanID    string         `json:"span_id"`
	ParentID  string         `json:"parent_id,omitempty"`
	Kind      string         `json:"kind,omitempty"`
	Name      string         `json:"name,omitempty"`
	Time      time.Time      `json:"time"`
	StartTime *time.Time     `json:"start_time,omitempty"`
	Status    string         `json:"status,omitempty"`
	Model     *string        `json:"model,omitempty"`
	Provider  *string        `json:"provider,omitempty"`
	ToolName  *string        `json:"tool_name,omitempty"`
	TokensIn  *int64         `json:"tokens_in,omitempty"`
	TokensOut *int64         `json:"tokens_out,omitempty"`
	CostUSD   *float64       `json:"cost_usd,omitempty"`
	Attrs     map[string]any `json:"attrs,omitempty"`
	Contents  []EventContent `json:"contents,omitempty"`
}

// EventContent is one optional body attached to an incremental span event.
type EventContent struct {
	Role      string `json:"role"`
	Body      string `json:"body"`
	MediaType string `json:"media_type,omitempty"`
}

// ApplySpanEvent merges only supplied fields. A repeated event id is a no-op.
func (s *Store) ApplySpanEvent(ctx context.Context, e SpanEvent, captureContent bool) (bool, error) {
	if e.CostUSD != nil && (math.IsNaN(*e.CostUSD) || math.IsInf(*e.CostUSD, 0) || *e.CostUSD < 0) {
		return false, ErrEventCost
	}
	payload, err := json.Marshal(e)
	if err != nil {
		return false, err
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(payload))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var prior string
	err = tx.QueryRowContext(ctx, `SELECT payload_hash FROM ingest_events WHERE event_id = ?`, e.EventID).Scan(&prior)
	if err == nil {
		if prior != hash {
			return false, ErrEventConflict
		}
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}

	starting := e.Type == "span.start" || e.Type == "span.complete"
	if starting {
		if _, err = tx.ExecContext(ctx, `INSERT INTO runs (id, source) VALUES (?, 'events') ON CONFLICT(id) DO NOTHING`, e.RunID); err != nil {
			return false, err
		}
		started := e.Time
		if e.Type == "span.complete" && e.StartTime != nil {
			started = *e.StartTime
		}
		var ended any
		status := "running"
		if e.Type == "span.complete" {
			ended, status = e.Time.UnixNano(), e.Status
		}
		attrs := Attrs{Raw: e.Attrs}
		if e.Model != nil {
			attrs.Model = *e.Model
		}
		if e.Provider != nil {
			attrs.Provider = *e.Provider
		}
		if e.ToolName != nil {
			attrs.ToolName = *e.ToolName
		}
		if !captureContent {
			attrs.Raw = metadataOnly(attrs.Raw)
		}
		data, marshalErr := json.Marshal(attrs)
		if marshalErr != nil {
			return false, marshalErr
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO spans
			(id, run_id, parent_id, kind, name, started_at, ended_at, tokens_in, tokens_out, cost_usd, status, attrs_json)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, e.SpanID, e.RunID, nullString(e.ParentID), e.Kind, e.Name,
			started.UnixNano(), ended, intValue(e.TokensIn), intValue(e.TokensOut), e.CostUSD, status, string(data))
		if err != nil {
			return false, fmt.Errorf("start span: %w", err)
		}
	} else {
		var data string
		var startedAt int64
		err = tx.QueryRowContext(ctx, `SELECT attrs_json, started_at FROM spans WHERE id = ? AND run_id = ?`, e.SpanID, e.RunID).Scan(&data, &startedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrSpanNotStarted
		}
		if err != nil {
			return false, err
		}
		if e.Type == "span.end" && e.Time.UnixNano() < startedAt {
			return false, ErrEventTime
		}
		var attrs Attrs
		if err = json.Unmarshal([]byte(data), &attrs); err != nil {
			return false, err
		}
		if e.Model != nil {
			attrs.Model = *e.Model
		}
		if e.Provider != nil {
			attrs.Provider = *e.Provider
		}
		if e.ToolName != nil {
			attrs.ToolName = *e.ToolName
		}
		if attrs.Raw == nil {
			attrs.Raw = make(map[string]any)
		}
		for k, v := range e.Attrs {
			attrs.Raw[k] = v
		}
		if !captureContent {
			attrs.Raw = metadataOnly(attrs.Raw)
		}
		merged, marshalErr := json.Marshal(attrs)
		if marshalErr != nil {
			return false, marshalErr
		}
		var ended any
		var status any
		if e.Type == "span.end" {
			ended, status = e.Time.UnixNano(), e.Status
		}
		_, err = tx.ExecContext(ctx, `UPDATE spans SET
			name = CASE WHEN ? != '' THEN ? ELSE name END,
			tokens_in = COALESCE(?, tokens_in), tokens_out = COALESCE(?, tokens_out),
			cost_usd = COALESCE(?, cost_usd),
			ended_at = COALESCE(?, ended_at), status = COALESCE(?, status),
			attrs_json = ?, analyzed = 0, revision = revision + 1
			WHERE id = ? AND run_id = ?`, e.Name, e.Name, e.TokensIn, e.TokensOut,
			e.CostUSD, ended, status, string(merged), e.SpanID, e.RunID)
		if err != nil {
			return false, err
		}
		// Usage or model changes invalidate our own estimate; provider costs remain authoritative.
		if e.CostUSD == nil && (e.Model != nil || e.TokensIn != nil || e.TokensOut != nil || len(e.Attrs) > 0) {
			if _, err = tx.ExecContext(ctx, `UPDATE spans SET cost_usd = NULL WHERE id = ? AND EXISTS
				(SELECT 1 FROM span_cost_provenance WHERE span_id = ? AND kind = 'estimate')`, e.SpanID, e.SpanID); err != nil {
				return false, err
			}
			if _, err = tx.ExecContext(ctx, `DELETE FROM span_cost_provenance WHERE span_id = ? AND kind = 'estimate'`, e.SpanID); err != nil {
				return false, err
			}
		}
	}
	if e.CostUSD != nil {
		if _, err = tx.ExecContext(ctx, `INSERT INTO span_cost_provenance
			(span_id, kind, price_source, price_version, checked_at, usage_source)
			VALUES (?, 'provider', 'application event', 'reported', ?, 'application')
			ON CONFLICT(span_id) DO UPDATE SET kind = excluded.kind,
			price_source = excluded.price_source, price_version = excluded.price_version,
			price_effective_at = NULL, checked_at = excluded.checked_at, usage_source = excluded.usage_source`,
			e.SpanID, time.Now().UnixNano()); err != nil {
			return false, err
		}
	}
	if !captureContent {
		if _, err = tx.ExecContext(ctx, `DELETE FROM contents WHERE span_id = ?`, e.SpanID); err != nil {
			return false, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM llm_cache WHERE span_id = ?`, e.SpanID); err != nil {
			return false, err
		}
	} else {
		for _, c := range e.Contents {
			mediaType := c.MediaType
			if mediaType == "" {
				mediaType = "text/plain"
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO contents (span_id, role, seq, body, media_type)
				VALUES (?, ?, (SELECT COALESCE(MAX(seq), -1) + 1 FROM contents WHERE span_id = ?), ?, ?)`,
				e.SpanID, c.Role, e.SpanID, c.Body, mediaType); err != nil {
				return false, err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO ingest_events (event_id, payload_hash, run_id, span_id, received_at) VALUES (?, ?, ?, ?, ?)`,
		e.EventID, hash, e.RunID, e.SpanID, time.Now().UnixNano()); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, refreshRunSQL, e.RunID); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	s.notify()
	return true, nil
}

func intValue(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

// EventRunCount counts runs that received at least one local HTTP event.
func (s *Store) EventRunCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT run_id) FROM ingest_events`).Scan(&count)
	return count, err
}
