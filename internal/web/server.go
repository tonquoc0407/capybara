package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/tonquoc0407/capybara/internal/incident"
	"github.com/tonquoc0407/capybara/internal/store"
)

// shutdownGrace bounds how long Serve waits for in-flight requests on exit.
const shutdownGrace = 2 * time.Second

// Serve runs the read-only view on addr until ctx ends, announcing the address
// it bound to — the only way to learn it when addr names port 0.
func Serve(ctx context.Context, st *store.Store, addr string, out io.Writer) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	if _, err := fmt.Fprintf(out, "http://%s\n", lis.Addr()); err != nil {
		lis.Close()
		return err
	}
	srv := &http.Server{Handler: Handler(st), ReadHeaderTimeout: 5 * time.Second}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(lis) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
		stop, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		return srv.Shutdown(stop)
	}
}

// Handler is the read-only site: the page itself and the data it fetches.
func Handler(st *store.Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		body, err := Page(nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(body)
	})
	mux.HandleFunc("GET /api/runs", func(w http.ResponseWriter, r *http.Request) {
		runs, err := Runs(r.Context(), st)
		writeJSON(w, runs, err)
	})
	mux.HandleFunc("GET /api/v1/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"mode": "local", "write": false, "ingest": false,
			"content_capture": "configured by receiver"}, nil)
	})
	mux.HandleFunc("GET /api/v1/projects", func(w http.ResponseWriter, r *http.Request) {
		projects, err := st.ListProjects(r.Context())
		writeJSON(w, projects, err)
	})
	mux.HandleFunc("GET /api/v1/operations", func(w http.ResponseWriter, r *http.Request) {
		ServeOperations(w, r, st, "")
	})
	mux.HandleFunc("GET /api/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := st.ResolveRunID(r.Context(), r.PathValue("id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		detail, err := Run(r.Context(), st, id)
		writeJSON(w, detail, err)
	})
	mux.HandleFunc("GET /api/v1/runs", func(w http.ResponseWriter, r *http.Request) {
		filter, err := ParseRunSearch(r, "")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		items, err := RunsForSearch(r.Context(), st, filter)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, struct {
			Items  []RunSummary `json:"items"`
			Limit  int          `json:"limit"`
			Offset int          `json:"offset"`
		}{items, filter.Limit, filter.Offset}, nil)
	})
	mux.HandleFunc("GET /api/v1/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := st.ResolveRunID(r.Context(), r.PathValue("id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		detail, err := run(r.Context(), st, id, false)
		writeJSON(w, detail, err)
	})
	mux.HandleFunc("GET /api/v1/runs/{id}/spans/{span}/contents", func(w http.ResponseWriter, r *http.Request) {
		id, err := st.ResolveRunID(r.Context(), r.PathValue("id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		span := r.PathValue("span")
		if !st.SpanBelongsToRun(r.Context(), span, id) {
			http.Error(w, "span not found", http.StatusNotFound)
			return
		}
		contents, err := SpanContents(r.Context(), st, span)
		writeJSON(w, contents, err)
	})
	mux.HandleFunc("GET /api/v1/incidents", func(w http.ResponseWriter, r *http.Request) {
		filter, err := ParseRunSearch(r, "")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("limit") == "" {
			filter.Limit = 100
		}
		groups, err := incident.ListForSearch(r.Context(), st, filter)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, groups, nil)
	})
	mux.HandleFunc("GET /api/v1/runs/{id}/incident-packet", func(w http.ResponseWriter, r *http.Request) {
		id, err := st.ResolveRunID(r.Context(), r.PathValue("id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		packet, err := incident.BuildPacket(r.Context(), st, id)
		writeJSON(w, packet, err)
	})
	mux.HandleFunc("GET /api/v1/runs/{id}/incident-packet.html", func(w http.ResponseWriter, r *http.Request) {
		id, err := st.ResolveRunID(r.Context(), r.PathValue("id"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		packet, err := incident.BuildPacket(r.Context(), st, id)
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
	mux.HandleFunc("GET /api/v1/runs/{id}/logs", func(w http.ResponseWriter, r *http.Request) {
		id, err := st.ResolveRunID(r.Context(), r.PathValue("id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		logs, err := st.TraceLogs(r.Context(), id)
		writeJSON(w, logs, err)
	})
	mux.HandleFunc("GET /api/v1/runs/{id}/scores", func(w http.ResponseWriter, r *http.Request) {
		id, err := st.ResolveRunID(r.Context(), r.PathValue("id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		scores, err := st.ListScores(r.Context(), id)
		writeJSON(w, scores, err)
	})
	mux.HandleFunc("GET /api/v1/datasets", func(w http.ResponseWriter, r *http.Request) {
		datasets, err := st.ListDatasets(r.Context(), r.URL.Query().Get("project"))
		writeJSON(w, datasets, err)
	})
	mux.HandleFunc("GET /api/v1/datasets/{id}/items", func(w http.ResponseWriter, r *http.Request) {
		items, err := st.ListDatasetItems(r.Context(), r.PathValue("id"))
		writeJSON(w, items, err)
	})
	mux.HandleFunc("GET /api/v1/datasets/{id}/experiments", func(w http.ResponseWriter, r *http.Request) {
		experiments, err := st.ListExperiments(r.Context(), r.PathValue("id"))
		writeJSON(w, experiments, err)
	})
	mux.HandleFunc("GET /api/v1/experiments/compare", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("baseline") == "" || q.Get("candidate") == "" || q.Get("score") == "" {
			http.Error(w, "baseline, candidate, and score are required", http.StatusBadRequest)
			return
		}
		comparison, err := st.CompareExperiments(r.Context(), q.Get("baseline"), q.Get("candidate"), q.Get("score"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, comparison, nil)
	})
	mux.HandleFunc("GET /api/v1/usage", func(w http.ResponseWriter, r *http.Request) {
		usage, err := st.UsageByProject(r.Context())
		writeJSON(w, usage, err)
	})
	mux.HandleFunc("GET /api/v1/runs/{id}/costs", func(w http.ResponseWriter, r *http.Request) {
		id, err := st.ResolveRunID(r.Context(), r.PathValue("id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		costs, err := st.CostProvenanceForRun(r.Context(), id)
		writeJSON(w, costs, err)
	})
	mux.HandleFunc("GET /api/v1/prompts", func(w http.ResponseWriter, r *http.Request) {
		project := r.URL.Query().Get("project")
		if project == "" {
			project = "default"
		}
		versions, err := st.ListPromptVersions(r.Context(), project, r.URL.Query().Get("name"))
		for i := range versions {
			versions[i].Content = ""
		}
		writeJSON(w, versions, err)
	})
	mux.HandleFunc("GET /api/v1/prompts/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, err := st.PromptVersion(r.Context(), r.PathValue("id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, p, nil)
	})
	mux.HandleFunc("GET /api/v1/runs/{id}/prompts", func(w http.ResponseWriter, r *http.Request) {
		id, err := st.ResolveRunID(r.Context(), r.PathValue("id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		refs, err := st.PromptReferencesForRun(r.Context(), id)
		for i := range refs {
			if refs[i].Prompt != nil {
				refs[i].Prompt.Content = ""
			}
		}
		writeJSON(w, refs, err)
	})
	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		ch, cancel := st.Subscribe()
		defer cancel()

		_, _ = fmt.Fprintf(w, ": connected\n\n")
		flusher.Flush()

		for {
			select {
			case <-r.Context().Done():
				return
			case _, ok := <-ch:
				if !ok {
					return
				}
				_, _ = fmt.Fprintf(w, "event: update\ndata: {}\n\n")
				flusher.Flush()
			}
		}
	})
	return mux
}

func writeJSON(w http.ResponseWriter, payload any, err error) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
