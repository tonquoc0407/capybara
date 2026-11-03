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

func TestHubQualityWritesRespectRoleProjectAndWriteHeader(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_, editor, _ := st.CreateHubToken(ctx, "alpha", "editor")
	_, viewer, _ := st.CreateHubToken(ctx, "alpha", "viewer")
	_, beta, _ := st.CreateHubToken(ctx, "beta", "editor")
	now := time.Now().UTC()
	err = st.WriteBatch(ctx, store.Batch{
		Source: "test", HubProject: "alpha",
		Spans:    []store.Span{{ID: "s", RunID: "r", Kind: store.KindAgent, Name: "agent", StartedAt: now, EndedAt: now.Add(time.Second), Status: "ok"}},
		Contents: []store.Content{{SpanID: "s", Role: "user", Body: "question"}, {SpanID: "s", Role: "assistant", Seq: 1, Body: "answer"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h := (Server{Store: st}).Handler()
	call := func(method, path, token, body string, writeHeader bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		if writeHeader {
			req.Header.Set("X-Capybara-Write", "1")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	if got := call("POST", "/api/v1/datasets", viewer, `{"name":"cases"}`, true); got.Code != http.StatusForbidden {
		t.Fatalf("viewer write: %d", got.Code)
	}
	if got := call("POST", "/api/v1/datasets", editor, `{"name":"cases"}`, false); got.Code != http.StatusForbidden {
		t.Fatalf("missing write header: %d", got.Code)
	}
	created := call("POST", "/api/v1/datasets", editor, `{"name":"cases"}`, true)
	if created.Code != http.StatusCreated {
		t.Fatalf("create dataset: %d %s", created.Code, created.Body.String())
	}
	var dataset store.Dataset
	if err := json.Unmarshal(created.Body.Bytes(), &dataset); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/datasets/" + dataset.ID + "/items"
	if got := call("POST", path, beta, `{"run_id":"r"}`, true); got.Code != http.StatusNotFound {
		t.Fatalf("cross-project item: %d", got.Code)
	}
	itemResponse := call("POST", path, editor, `{"run_id":"r"}`, true)
	if itemResponse.Code != http.StatusCreated {
		t.Fatalf("create item: %d %s", itemResponse.Code, itemResponse.Body.String())
	}
	var item store.DatasetItem
	if err := json.Unmarshal(itemResponse.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	expectedPath := path + "/" + item.ID + "/expected"
	for _, attempt := range []struct {
		token  string
		status int
	}{{viewer, 403}, {beta, 404}, {editor, 200}} {
		if got := call("POST", expectedPath, attempt.token, `{"expected":"corrected answer"}`, true); got.Code != attempt.status {
			t.Fatalf("expected edit role/project: %d %s", got.Code, got.Body.String())
		}
	}
	if got := call("POST", "/api/v1/runs/r/scores", beta, `{"name":"quality","value":1}`, true); got.Code != http.StatusNotFound {
		t.Fatalf("cross-project score: %d", got.Code)
	}
	score := call("POST", "/api/v1/runs/r/scores", editor, `{"item_id":"`+item.ID+`","name":"quality","value":1}`, true)
	if score.Code != http.StatusCreated {
		t.Fatalf("score: %d %s", score.Code, score.Body.String())
	}
	expResponse := call("POST", "/api/v1/experiments", editor, `{"dataset_id":"`+dataset.ID+`","name":"candidate"}`, true)
	if expResponse.Code != http.StatusCreated {
		t.Fatalf("experiment: %d %s", expResponse.Code, expResponse.Body.String())
	}
	var experiment store.Experiment
	if err := json.Unmarshal(expResponse.Body.Bytes(), &experiment); err != nil {
		t.Fatal(err)
	}
	if got := call("POST", expectedPath, editor, `{"expected":"changed"}`, true); got.Code != http.StatusConflict {
		t.Fatalf("edited frozen case: %d %s", got.Code, got.Body.String())
	}
	clonePath := "/api/v1/datasets/" + dataset.ID + "/clone"
	if got := call("POST", clonePath, beta, `{"name":"stolen"}`, true); got.Code != 404 {
		t.Fatalf("cross-project clone: %d", got.Code)
	}
	if got := call("POST", clonePath, editor, `{"name":"revised"}`, true); got.Code != 201 {
		t.Fatalf("clone: %d %s", got.Code, got.Body.String())
	}
	result := call("POST", "/api/v1/experiments/"+experiment.ID+"/results", editor,
		`{"item_id":"`+item.ID+`","run_id":"r","score_name":"quality"}`, true)
	if result.Code != http.StatusNoContent {
		t.Fatalf("experiment result: %d %s", result.Code, result.Body.String())
	}
	prompt := call("POST", "/api/v1/prompts", editor, `{"name":"system","content":"hello"}`, true)
	if prompt.Code != http.StatusCreated {
		t.Fatalf("prompt: %d %s", prompt.Code, prompt.Body.String())
	}
}
