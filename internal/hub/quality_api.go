package hub

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/tonquoc0407/capybara/internal/store"
)

func readJSON(w http.ResponseWriter, r *http.Request, into any) bool {
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		http.Error(w, "expected exactly one JSON value", http.StatusBadRequest)
		return false
	}
	return true
}

func writeCreated(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(value)
}

func (s Server) createDataset(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	d, err := s.Store.CreateDataset(r.Context(), principal(r).Project, body.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeCreated(w, d)
}

func (s Server) addDatasetItem(w http.ResponseWriter, r *http.Request) {
	dataset := r.PathValue("id")
	if !s.Store.DatasetBelongsToProject(r.Context(), dataset, principal(r).Project) {
		http.NotFound(w, r)
		return
	}
	var body struct {
		RunID string `json:"run_id"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	runID, err := s.Store.ResolveProjectRunID(r.Context(), principal(r).Project, body.RunID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	item, err := s.Store.AddDatasetItem(r.Context(), dataset, runID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeCreated(w, item)
}

func (s Server) setDatasetExpected(w http.ResponseWriter, r *http.Request) {
	dataset := r.PathValue("id")
	if !s.Store.DatasetBelongsToProject(r.Context(), dataset, principal(r).Project) {
		http.NotFound(w, r)
		return
	}
	var body struct {
		Expected string `json:"expected"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	item, err := s.Store.SetDatasetExpected(r.Context(), dataset, r.PathValue("item"), body.Expected)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, store.ErrDatasetFrozen) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, item, nil)
}

func (s Server) cloneDataset(w http.ResponseWriter, r *http.Request) {
	dataset := r.PathValue("id")
	if !s.Store.DatasetBelongsToProject(r.Context(), dataset, principal(r).Project) {
		http.NotFound(w, r)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	d, err := s.Store.CloneDataset(r.Context(), dataset, body.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeCreated(w, d)
}

func (s Server) addScore(w http.ResponseWriter, r *http.Request) {
	runID, ok := s.runID(w, r)
	if !ok {
		return
	}
	var body struct {
		ItemID  string   `json:"item_id"`
		Name    string   `json:"name"`
		Value   *float64 `json:"value"`
		Source  string   `json:"source"`
		Comment string   `json:"comment"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if body.Value == nil {
		http.Error(w, "score value is required", http.StatusBadRequest)
		return
	}
	score, err := s.Store.AddScore(r.Context(), runID, body.ItemID, body.Name, *body.Value, body.Source, body.Comment)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeCreated(w, score)
}

func (s Server) createExperiment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DatasetID string `json:"dataset_id"`
		Name      string `json:"name"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if !s.Store.DatasetBelongsToProject(r.Context(), body.DatasetID, principal(r).Project) {
		http.NotFound(w, r)
		return
	}
	experiment, err := s.Store.CreateExperiment(r.Context(), body.DatasetID, body.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeCreated(w, experiment)
}

func (s Server) recordExperiment(w http.ResponseWriter, r *http.Request) {
	experiment := r.PathValue("id")
	if !s.Store.ExperimentBelongsToProject(r.Context(), experiment, principal(r).Project) {
		http.NotFound(w, r)
		return
	}
	var body struct {
		ItemID    string `json:"item_id"`
		RunID     string `json:"run_id"`
		ScoreName string `json:"score_name"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	runID, err := s.Store.ResolveProjectRunID(r.Context(), principal(r).Project, body.RunID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.Store.RecordExperimentResult(r.Context(), experiment, body.ItemID, runID, body.ScoreName); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s Server) registerPrompt(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name       string `json:"name"`
		Content    string `json:"content"`
		SourcePath string `json:"source_path"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	p, err := s.Store.RegisterPrompt(r.Context(), principal(r).Project, body.Name, body.Content, body.SourcePath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeCreated(w, p)
}
