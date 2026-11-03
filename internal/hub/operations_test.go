package hub

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

func TestOperationsCannotOverrideTokenProject(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "ops.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_, secret, err := st.CreateHubToken(ctx, "alpha", "viewer")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Add(-time.Hour)
	for _, project := range []string{"alpha", "beta"} {
		cost := 1.0
		if project == "beta" {
			cost = 99
		}
		err := st.WriteBatch(ctx, store.Batch{Source: "test", Spans: []store.Span{{ID: project + "-span", RunID: project,
			Kind: store.KindAgent, Name: "agent", Status: "ok", StartedAt: at, EndedAt: at.Add(time.Second), CostUSD: &cost}},
			RunContexts: map[string]store.RunContext{project: {Project: project, Release: project + "-release"}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	h := (Server{Store: st}).Handler()
	request := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Authorization", "Bearer "+secret)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := request("/api/v1/operations?project=beta")
	var report store.OperationsReport
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatalf("%d: %s: %v", w.Code, w.Body.String(), err)
	}
	if w.Code != 200 || report.Project != "alpha" || report.Total.Runs != 1 || report.Total.KnownCostUSD != 1 ||
		len(report.Releases) != 1 || report.Releases[0].Key != "alpha-release" {
		t.Fatalf("cross-project report: %+v", report)
	}
	if got := request("/api/v1/operations?from=not-a-date"); got.Code != 400 {
		t.Fatalf("invalid range: %d", got.Code)
	}
	if got := request("/api/v1/operations?from=2026-01-01T00:00:00Z&to=2026-03-01T00:00:00Z"); got.Code != 400 {
		t.Fatalf("unbounded range: %d", got.Code)
	}
}
