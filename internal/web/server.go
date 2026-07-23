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
	mux.HandleFunc("GET /api/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := st.ResolveRunID(r.Context(), r.PathValue("id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		detail, err := Run(r.Context(), st, id)
		writeJSON(w, detail, err)
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
