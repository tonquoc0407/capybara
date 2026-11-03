package otlp

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"

	"github.com/tonquoc0407/capybara/internal/store"
)

const maxEventBody = 1 << 20

func (r *Receiver) handleEvent(w http.ResponseWriter, req *http.Request) {
	host, _, addrErr := net.SplitHostPort(req.RemoteAddr)
	if addrErr != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		http.Error(w, "local requests only", http.StatusForbidden)
		return
	}
	if !r.acquire() {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "ingest busy; retry", http.StatusServiceUnavailable)
		return
	}
	defer r.release()
	mediaType, _, mediaErr := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if mediaErr != nil || mediaType != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, req.Body, maxEventBody))
	dec.DisallowUnknownFields()
	var e store.SpanEvent
	if err := dec.Decode(&e); err != nil {
		var sizeErr *http.MaxBytesError
		if errors.As(err, &sizeErr) {
			http.Error(w, "event exceeds 1 MiB", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		}
		return
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		var sizeErr *http.MaxBytesError
		if errors.As(err, &sizeErr) {
			http.Error(w, "event exceeds 1 MiB", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "expected one JSON event", http.StatusBadRequest)
		return
	}
	if err := validateEvent(e); err != "" {
		http.Error(w, err, http.StatusBadRequest)
		return
	}
	created, err := r.store.ApplySpanEvent(req.Context(), e, r.capture)
	if err != nil {
		if errors.Is(err, store.ErrEventTime) || errors.Is(err, store.ErrEventCost) {
			http.Error(w, err.Error(), http.StatusBadRequest)
		} else if errors.Is(err, store.ErrEventConflict) || errors.Is(err, store.ErrSpanNotStarted) || strings.Contains(err.Error(), "constraint failed") {
			http.Error(w, err.Error(), http.StatusConflict)
		} else if strings.Contains(err.Error(), "SQLITE_BUSY") || strings.Contains(err.Error(), "database is locked") {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "ingest busy; retry", http.StatusServiceUnavailable)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if created {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"stored":true}`))
		return
	}
	_, _ = w.Write([]byte(`{"stored":false,"duplicate":true}`))
}

func validateEvent(e store.SpanEvent) string {
	for _, v := range []string{e.EventID, e.RunID, e.SpanID} {
		if v == "" || len(v) > 256 {
			return "event_id, run_id and span_id must be 1–256 characters"
		}
	}
	if e.Time.IsZero() {
		return "time is required (RFC3339)"
	}
	if e.TokensIn != nil && *e.TokensIn < 0 || e.TokensOut != nil && *e.TokensOut < 0 {
		return "token counts must be nonnegative"
	}
	if e.CostUSD != nil && *e.CostUSD < 0 {
		return "cost_usd must be nonnegative"
	}
	switch e.Type {
	case "span.start", "span.complete":
		if e.Name == "" || len(e.Name) > 512 {
			return "name is required (up to 512 characters)"
		}
		switch e.Kind {
		case "agent", "llm", "tool", "retrieval", "other":
		default:
			return "kind must be agent, llm, tool, retrieval or other"
		}
		if e.Type == "span.complete" && e.StartTime != nil && e.StartTime.After(e.Time) {
			return "start_time must precede time"
		}
		if e.Type == "span.start" && e.StartTime != nil {
			return "start_time is only valid on span.complete"
		}
	case "span.update", "span.end":
		if e.ParentID != "" || e.Kind != "" || e.StartTime != nil {
			return "parent_id, kind and start_time are only valid when starting a span"
		}
	default:
		return "unknown event type"
	}
	if e.Type == "span.end" || e.Type == "span.complete" {
		if e.Status != "ok" && e.Status != "error" {
			return "end status must be ok or error"
		}
	} else if e.Status != "" {
		return "status is only valid on span.end or span.complete"
	}
	for _, c := range e.Contents {
		if c.Role == "" {
			return "content role is required"
		}
	}
	return ""
}
