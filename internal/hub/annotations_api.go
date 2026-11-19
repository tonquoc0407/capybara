package hub

import (
	"encoding/json"
	"net/http"
)

type annotationRequest struct {
	Status string `json:"status"`
	Note   string `json:"note"`
	Author string `json:"author"`
}

func (s Server) listAnnotations(w http.ResponseWriter, r *http.Request) {
	id, ok := s.runID(w, r)
	if !ok {
		return
	}
	items, err := s.Store.IncidentAnnotations(r.Context(), id)
	writeJSON(w, items, err)
}

func (s Server) addAnnotation(w http.ResponseWriter, r *http.Request) {
	id, ok := s.runID(w, r)
	if !ok {
		return
	}
	var body annotationRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(&body); err != nil {
		http.Error(w, "invalid annotation JSON", http.StatusBadRequest)
		return
	}
	if body.Author == "" {
		body.Author = principal(r).ID
	}
	item, err := s.Store.AddIncidentAnnotation(r.Context(), id, body.Status, body.Note, body.Author)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(item)
}
