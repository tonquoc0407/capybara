// Package hub serves a single-node, authenticated capybara workspace.
package hub

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/incident"
	"github.com/tonquoc0407/capybara/internal/ingest/otlp"
	"github.com/tonquoc0407/capybara/internal/store"
	"github.com/tonquoc0407/capybara/internal/web"
)

const maxBody = 64 << 20

// Server serves an authenticated workspace backed by one local store.
type Server struct {
	Store          *store.Store
	CaptureContent bool
	Retention      time.Duration
	RedactPattern  *regexp.Regexp
	Alerts         *AlertConfig
}

// Serve listens on a loopback address until the context is cancelled.
func (s Server) Serve(ctx context.Context, addr string, out io.Writer) error {
	if s.Retention < 0 {
		return errors.New("retention must be non-negative")
	}
	if s.Alerts != nil {
		if err := s.Alerts.validate(); err != nil {
			return err
		}
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return errors.New("hub must bind localhost; put a TLS reverse proxy in front for remote access")
		}
	}
	analyzer, err := analyze.New(s.Store)
	if err != nil {
		return err
	}
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	if s.Retention > 0 {
		if err := s.applyRetention(ctx); err != nil {
			lis.Close()
			return err
		}
	}
	if _, err := fmt.Fprintf(out, "http://%s\n", lis.Addr()); err != nil {
		lis.Close()
		return err
	}
	runCtx, cancelRun := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() { cancelRun(); workers.Wait() }()
	srv := &http.Server{
		Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second,
		BaseContext: func(net.Listener) context.Context { return runCtx },
	}
	analysisDone := make(chan error, 1)
	workers.Add(1)
	go func() { defer workers.Done(); analysisDone <- analyzer.Watch(runCtx) }()
	if s.Alerts != nil && s.Alerts.WebhookURL != "" {
		monitor := &alertMonitor{store: s.Store, config: *s.Alerts, client: &http.Client{Timeout: 5 * time.Second}}
		workers.Add(1)
		go func() {
			defer workers.Done()
			ticker := time.NewTicker(s.Alerts.Interval)
			defer ticker.Stop()
			for {
				select {
				case <-runCtx.Done():
					return
				case at := <-ticker.C:
					if _, err := monitor.check(runCtx, at); err != nil {
						fmt.Fprintf(out, "alert: %v\n", err)
					}
				}
			}
		}()
	}
	if s.Retention > 0 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			ticker := time.NewTicker(time.Hour)
			defer ticker.Stop()
			for {
				select {
				case <-runCtx.Done():
					return
				case <-ticker.C:
					if err := s.applyRetention(runCtx); err != nil {
						fmt.Fprintf(out, "retention: %v\n", err)
					}
				}
			}
		}()
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(lis) }()
	select {
	case err := <-analysisDone:
		cancelRun()
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return errors.Join(err, srv.Shutdown(stop))
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(stop)
	}
}

func (s Server) applyRetention(ctx context.Context) error {
	if s.Retention <= 0 {
		return nil
	}
	n, err := s.Store.Prune(ctx, store.PruneOptions{OlderThan: s.Retention})
	if err != nil {
		return err
	}
	if n > 0 {
		return s.Store.RecordAudit(ctx, "system", "retention.prune", "*", fmt.Sprintf("%d runs", n))
	}
	return nil
}

// Handler checks a Bearer token or HTTP Basic password before every route.
// Browser Basic auth keeps the embedded page and EventSource on one origin.
func (s Server) Handler() http.Handler {
	mux := http.NewServeMux()
	ingestSlots := make(chan struct{}, 8)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		body, err := web.Page(nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(body)
	})
	mux.HandleFunc("GET /api/runs", func(w http.ResponseWriter, r *http.Request) {
		runs, err := web.RunsForProject(r.Context(), s.Store, principal(r).Project, 100, 0)
		writeJSON(w, runs, err)
	})
	mux.HandleFunc("GET /api/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := s.runID(w, r)
		if !ok {
			return
		}
		data, err := web.Run(r.Context(), s.Store, id)
		writeJSON(w, data, err)
	})
	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		// Emit on a fixed cadence. A store-wide subscription would reveal the
		// timing of writes in other projects to this project's viewer.
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		_, _ = io.WriteString(w, ": connected\n\n")
		flusher.Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				_, _ = io.WriteString(w, "event: update\ndata: {}\n\n")
				flusher.Flush()
			}
		}
	})
	mux.HandleFunc("GET /api/v1/projects", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []string{principal(r).Project}, nil)
	})
	mux.HandleFunc("GET /api/v1/operations", func(w http.ResponseWriter, r *http.Request) {
		web.ServeOperations(w, r, s.Store, principal(r).Project)
	})
	mux.HandleFunc("GET /api/v1/capabilities", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"mode": "hub", "project": principal(r).Project, "ingest": true,
			"role": principal(r).Role, "write": principal(r).Role != "viewer",
			"content_capture": s.CaptureContent, "retention_hours": s.Retention.Hours(),
		}, nil)
	})
	mux.HandleFunc("GET /api/v1/runs", func(w http.ResponseWriter, r *http.Request) {
		filter, err := web.ParseRunSearch(r, principal(r).Project)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		runs, err := web.RunsForSearch(r.Context(), s.Store, filter)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, struct {
			Items  []web.RunSummary `json:"items"`
			Limit  int              `json:"limit"`
			Offset int              `json:"offset"`
		}{runs, filter.Limit, filter.Offset}, nil)
	})
	mux.HandleFunc("GET /api/v1/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := s.runID(w, r)
		if !ok {
			return
		}
		data, err := web.RunMetadata(r.Context(), s.Store, id)
		writeJSON(w, data, err)
	})
	mux.HandleFunc("GET /api/v1/runs/{id}/spans/{span}/contents", func(w http.ResponseWriter, r *http.Request) {
		id, ok := s.runID(w, r)
		if !ok {
			return
		}
		span := r.PathValue("span")
		if !s.Store.SpanBelongsToRun(r.Context(), span, id) {
			http.NotFound(w, r)
			return
		}
		content, err := web.SpanContents(r.Context(), s.Store, span)
		writeJSON(w, content, err)
	})
	mux.HandleFunc("GET /api/v1/runs/{id}/logs", func(w http.ResponseWriter, r *http.Request) {
		id, ok := s.runID(w, r)
		if !ok {
			return
		}
		logs, err := s.Store.TraceLogs(r.Context(), id)
		writeJSON(w, logs, err)
	})
	mux.HandleFunc("GET /api/v1/runs/{id}/costs", func(w http.ResponseWriter, r *http.Request) {
		id, ok := s.runID(w, r)
		if !ok {
			return
		}
		costs, err := s.Store.CostProvenanceForRun(r.Context(), id)
		writeJSON(w, costs, err)
	})
	mux.HandleFunc("GET /api/v1/runs/{id}/scores", func(w http.ResponseWriter, r *http.Request) {
		id, ok := s.runID(w, r)
		if !ok {
			return
		}
		scores, err := s.Store.ListScores(r.Context(), id)
		writeJSON(w, scores, err)
	})
	mux.HandleFunc("GET /api/v1/runs/{id}/incident-packet", func(w http.ResponseWriter, r *http.Request) {
		id, ok := s.runID(w, r)
		if !ok {
			return
		}
		packet, err := incident.BuildPacket(r.Context(), s.Store, id)
		writeJSON(w, packet, err)
	})
	mux.HandleFunc("GET /api/v1/runs/{id}/incident-packet.html", func(w http.ResponseWriter, r *http.Request) {
		id, ok := s.runID(w, r)
		if !ok {
			return
		}
		packet, err := incident.BuildPacket(r.Context(), s.Store, id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		body, err := incident.PacketHTML(packet)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(body)
	})
	mux.HandleFunc("GET /api/v1/runs/{id}/prompts", func(w http.ResponseWriter, r *http.Request) {
		id, ok := s.runID(w, r)
		if !ok {
			return
		}
		refs, err := s.Store.PromptReferencesForRun(r.Context(), id)
		for i := range refs {
			if refs[i].Prompt != nil {
				refs[i].Prompt.Content = ""
			}
		}
		writeJSON(w, refs, err)
	})
	mux.HandleFunc("GET /api/v1/usage", func(w http.ResponseWriter, r *http.Request) {
		usage, err := s.Store.UsageByProject(r.Context())
		if err != nil {
			writeJSON(w, nil, err)
			return
		}
		out := []store.UsageSummary{}
		for _, u := range usage {
			if u.Project == principal(r).Project {
				out = append(out, u)
			}
		}
		writeJSON(w, out, nil)
	})
	mux.HandleFunc("GET /api/v1/incidents", func(w http.ResponseWriter, r *http.Request) {
		filter, err := web.ParseRunSearch(r, principal(r).Project)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("limit") == "" {
			filter.Limit = 100
		}
		groups, err := incident.ListForSearch(r.Context(), s.Store, filter)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, groups, nil)
	})
	mux.HandleFunc("GET /api/v1/datasets", func(w http.ResponseWriter, r *http.Request) {
		items, err := s.Store.ListDatasets(r.Context(), principal(r).Project)
		writeJSON(w, items, err)
	})
	mux.HandleFunc("POST /api/v1/datasets", s.createDataset)
	mux.HandleFunc("POST /api/v1/datasets/{id}/items", s.addDatasetItem)
	mux.HandleFunc("POST /api/v1/datasets/{id}/items/{item}/expected", s.setDatasetExpected)
	mux.HandleFunc("POST /api/v1/datasets/{id}/clone", s.cloneDataset)
	mux.HandleFunc("POST /api/v1/runs/{id}/scores", s.addScore)
	mux.HandleFunc("POST /api/v1/experiments", s.createExperiment)
	mux.HandleFunc("POST /api/v1/experiments/{id}/results", s.recordExperiment)
	mux.HandleFunc("POST /api/v1/prompts", s.registerPrompt)
	mux.HandleFunc("GET /api/v1/datasets/{id}/items", func(w http.ResponseWriter, r *http.Request) {
		if !s.Store.DatasetBelongsToProject(r.Context(), r.PathValue("id"), principal(r).Project) {
			http.NotFound(w, r)
			return
		}
		items, err := s.Store.ListDatasetItems(r.Context(), r.PathValue("id"))
		writeJSON(w, items, err)
	})
	mux.HandleFunc("GET /api/v1/datasets/{id}/experiments", func(w http.ResponseWriter, r *http.Request) {
		if !s.Store.DatasetBelongsToProject(r.Context(), r.PathValue("id"), principal(r).Project) {
			http.NotFound(w, r)
			return
		}
		items, err := s.Store.ListExperiments(r.Context(), r.PathValue("id"))
		writeJSON(w, items, err)
	})
	mux.HandleFunc("GET /api/v1/experiments/compare", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if !s.Store.ExperimentBelongsToProject(r.Context(), q.Get("baseline"), principal(r).Project) ||
			!s.Store.ExperimentBelongsToProject(r.Context(), q.Get("candidate"), principal(r).Project) {
			http.NotFound(w, r)
			return
		}
		comparison, err := s.Store.CompareExperiments(r.Context(), q.Get("baseline"), q.Get("candidate"), q.Get("score"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, comparison, nil)
	})
	mux.HandleFunc("GET /api/v1/prompts", func(w http.ResponseWriter, r *http.Request) {
		versions, err := s.Store.ListPromptVersions(r.Context(), principal(r).Project, r.URL.Query().Get("name"))
		for i := range versions {
			versions[i].Content = ""
		}
		writeJSON(w, versions, err)
	})
	mux.HandleFunc("GET /api/v1/prompts/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.Store.PromptVersion(r.Context(), r.PathValue("id"))
		if err != nil || p.Project != principal(r).Project {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, p, nil)
	})
	mux.HandleFunc("GET /api/v1/audit", func(w http.ResponseWriter, r *http.Request) {
		if principal(r).Role != "admin" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		events, err := s.Store.AuditEvents(r.Context(), principal(r).Project, 100)
		writeJSON(w, events, err)
	})
	mux.HandleFunc("POST /v1/traces", s.traces)
	mux.HandleFunc("POST /v1/logs", s.logs)
	mux.HandleFunc("POST /v1/metrics", s.metrics)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		secret := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if secret == r.Header.Get("Authorization") {
			_, secret, _ = r.BasicAuth()
		}
		p, err := s.Store.AuthenticateHubToken(r.Context(), secret)
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Basic realm="capybara hub"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodPost && p.Role == "viewer" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost && (r.URL.Path == "/v1/traces" || r.URL.Path == "/v1/logs" || r.URL.Path == "/v1/metrics") {
			select {
			case ingestSlots <- struct{}{}:
				defer func() { <-ingestSlots }()
			default:
				w.Header().Set("Retry-After", "1")
				http.Error(w, "ingest busy; retry", http.StatusServiceUnavailable)
				return
			}
		}
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/") && r.Header.Get("X-Capybara-Write") != "1" {
			http.Error(w, "X-Capybara-Write: 1 required", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	})
}

type principalKey struct{}

func principal(r *http.Request) store.HubToken {
	return r.Context().Value(principalKey{}).(store.HubToken)
}

func (s Server) runID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id, err := s.Store.ResolveProjectRunID(r.Context(), principal(r).Project, r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return "", false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, v any, err error) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func readOTLP(w http.ResponseWriter, r *http.Request) ([]byte, string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	var reader io.Reader = r.Body
	if enc := r.Header.Get("Content-Encoding"); enc != "" {
		if enc != "gzip" {
			http.Error(w, "unsupported content encoding", http.StatusUnsupportedMediaType)
			return nil, "", false
		}
		gz, err := gzip.NewReader(r.Body)
		if err != nil {
			http.Error(w, "invalid gzip", http.StatusBadRequest)
			return nil, "", false
		}
		defer gz.Close()
		reader = gz
	}
	contentType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if contentType != "application/json" && contentType != "application/x-protobuf" {
		http.Error(w, "unsupported content type", http.StatusUnsupportedMediaType)
		return nil, "", false
	}
	raw, err := io.ReadAll(io.LimitReader(reader, maxBody+1))
	if err != nil || len(raw) > maxBody {
		http.Error(w, "invalid or oversized body", http.StatusRequestEntityTooLarge)
		return nil, "", false
	}
	return raw, contentType, true
}

func decode(raw []byte, contentType string, proto, jsonUn func([]byte) error) error {
	if contentType == "application/json" {
		return jsonUn(raw)
	}
	return proto(raw)
}

func respondOTLP(w http.ResponseWriter, contentType string, proto, jsonMar func() ([]byte, error)) {
	marshal := proto
	if contentType == "application/json" {
		marshal = jsonMar
	}
	raw, err := marshal()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(raw)
}

func (s Server) traces(w http.ResponseWriter, r *http.Request) {
	raw, contentType, ok := readOTLP(w, r)
	if !ok {
		return
	}
	req := ptraceotlp.NewExportRequest()
	if err := decode(raw, contentType, req.UnmarshalProto, req.UnmarshalJSON); err != nil {
		http.Error(w, "invalid OTLP trace", http.StatusBadRequest)
		return
	}
	b := otlp.ToBatch(req.Traces(), s.CaptureContent)
	s.redactBatch(&b)
	b.HubProject = principal(r).Project
	if err := s.Store.WriteBatch(r.Context(), b); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	resp := ptraceotlp.NewExportResponse()
	respondOTLP(w, contentType, resp.MarshalProto, resp.MarshalJSON)
}

func (s Server) logs(w http.ResponseWriter, r *http.Request) {
	raw, contentType, ok := readOTLP(w, r)
	if !ok {
		return
	}
	req := plogotlp.NewExportRequest()
	if err := decode(raw, contentType, req.UnmarshalProto, req.UnmarshalJSON); err != nil {
		http.Error(w, "invalid OTLP log", http.StatusBadRequest)
		return
	}
	logs := otlp.ToLogs(req.Logs(), s.CaptureContent)
	s.redactLogs(logs)
	if err := s.Store.PutProjectTraceLogs(r.Context(), "otlp", principal(r).Project, logs); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	resp := plogotlp.NewExportResponse()
	respondOTLP(w, contentType, resp.MarshalProto, resp.MarshalJSON)
}

func (s Server) metrics(w http.ResponseWriter, r *http.Request) {
	raw, contentType, ok := readOTLP(w, r)
	if !ok {
		return
	}
	req := pmetricotlp.NewExportRequest()
	if err := decode(raw, contentType, req.UnmarshalProto, req.UnmarshalJSON); err != nil {
		http.Error(w, "invalid OTLP metric", http.StatusBadRequest)
		return
	}
	if err := s.Store.PutProjectResourceSamples(r.Context(), "otlp", principal(r).Project, otlp.ToSamples(req.Metrics())); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	resp := pmetricotlp.NewExportResponse()
	respondOTLP(w, contentType, resp.MarshalProto, resp.MarshalJSON)
}
