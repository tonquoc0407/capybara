package analyze

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

func TestLearnedToolSchemasStayWithinProject(t *testing.T) {
	ctx := context.Background()
	st := openTemp(t)
	a := newAnalyzer(t, st)
	for i, sample := range []struct{ id, project, output string }{
		{"alpha-1", "alpha", `{"alpha_private":1}`},
		{"beta-1", "beta", `{"beta_field":true}`},
		{"alpha-2", "alpha", `{"alpha_private":2}`},
	} {
		at := t0.Add(time.Duration(i) * time.Minute)
		sp := store.Span{ID: sample.id + "-tool", RunID: sample.id, Kind: store.KindTool, Name: "fetch",
			Status: "ok", StartedAt: at, EndedAt: at.Add(time.Second), Attrs: store.Attrs{ToolName: "fetch"}}
		if err := st.WriteBatch(ctx, store.Batch{Source: "test", Spans: []store.Span{sp},
			RunContexts: map[string]store.RunContext{sample.id: {Project: sample.project}},
			Contents:    []store.Content{{SpanID: sp.ID, Role: "output", Body: sample.output}}}); err != nil {
			t.Fatal(err)
		}
		if err := a.Sweep(ctx); err != nil {
			t.Fatal(err)
		}
		if fs := runFindings(t, st, sample.id); len(fs) != 0 {
			t.Fatalf("foreign schema affected %s: %+v", sample.id, fs)
		}
	}
	for _, project := range []string{"alpha", "beta"} {
		ts, err := st.LatestProjectToolSchema(ctx, project, "fetch")
		if err != nil || ts == nil || ts.Project != project || ts.Version != 1 || !strings.Contains(ts.Schema, project+"_") {
			t.Fatalf("scoped schema %s: %+v, %v", project, ts, err)
		}
	}
	if ts, err := st.LatestToolSchema(ctx, "fetch"); err != nil || ts != nil {
		t.Fatalf("schema leaked to default project: %+v, %v", ts, err)
	}
	// A restart must recover the project's version rather than a global counter.
	a = newAnalyzer(t, st)
	sp := store.Span{ID: "beta-2-tool", RunID: "beta-2", Kind: store.KindTool, Name: "fetch", Status: "ok",
		StartedAt: t0.Add(4 * time.Minute), EndedAt: t0.Add(4*time.Minute + time.Second), Attrs: store.Attrs{ToolName: "fetch"}}
	if err := st.WriteBatch(ctx, store.Batch{Source: "test", Spans: []store.Span{sp},
		RunContexts: map[string]store.RunContext{sp.RunID: {Project: "beta"}},
		Contents:    []store.Content{{SpanID: sp.ID, Role: "output", Body: `{"new_field":true}`}}}); err != nil {
		t.Fatal(err)
	}
	if err := a.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	fs := runFindings(t, st, sp.RunID)
	if len(fs) != 1 || fs[0].Type != "drift" || strings.Contains(fs[0].Detail, "alpha_private") {
		t.Fatalf("wrong project evidence after restart: %+v", fs)
	}
	if ts, err := st.LatestProjectToolSchema(ctx, "beta", "fetch"); err != nil || ts == nil || ts.Version != 2 {
		t.Fatalf("restart version: %+v, %v", ts, err)
	}
}
