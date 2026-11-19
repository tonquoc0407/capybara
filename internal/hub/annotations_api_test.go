package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

func TestIncidentAnnotationsAPI(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_, secret, err := st.CreateHubToken(ctx, "checkout", "editor")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	if err := st.WriteBatch(ctx, store.Batch{Source: "test", RunContexts: map[string]store.RunContext{"run-1": {Project: "checkout"}}, Spans: []store.Span{{ID: "span-1", RunID: "run-1", Kind: store.KindAgent, Name: "agent", Status: "ok", StartedAt: at, EndedAt: at}}}); err != nil {
		t.Fatal(err)
	}
	h := (Server{Store: st}).Handler()
	call := func(method, path string, body []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+secret)
		if method == http.MethodPost {
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Capybara-Write", "1")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	post := call(http.MethodPost, "/api/v1/runs/run-1/annotations", []byte(`{"status":"investigating","note":"checked retry path"}`))
	if post.Code != http.StatusCreated {
		t.Fatalf("post status=%d body=%s", post.Code, post.Body.String())
	}
	var item store.IncidentAnnotation
	if err := json.Unmarshal(post.Body.Bytes(), &item); err != nil || item.Status != "investigating" || item.Author == "" {
		t.Fatalf("post item=%+v err=%v", item, err)
	}
	get := call(http.MethodGet, "/api/v1/runs/run-1/annotations", nil)
	if get.Code != http.StatusOK || !bytes.Contains(get.Body.Bytes(), []byte("checked retry path")) {
		t.Fatalf("get status=%d body=%s", get.Code, get.Body.String())
	}
}
