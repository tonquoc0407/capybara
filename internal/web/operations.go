package web

import (
	"net/http"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

// ServeOperations shares the dashboard contract between local and hub modes.
// forcedProject is supplied by authenticated hub ownership, never by the URL.
func ServeOperations(w http.ResponseWriter, r *http.Request, st *store.Store, forcedProject string) {
	search, err := ParseRunSearch(r, forcedProject)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f := store.OperationsFilter{Project: search.Project, Environment: search.Environment,
		Release: search.Release, From: search.From, To: search.To}
	if f.Project == "" {
		f.Project = "default"
	}
	if f.To.IsZero() {
		f.To = time.Now().UTC()
	}
	if f.From.IsZero() {
		f.From = f.To.Add(-24 * time.Hour)
	}
	if err := f.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	report, err := st.Operations(r.Context(), f)
	writeJSON(w, report, err)
}
