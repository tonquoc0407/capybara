package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

var t0 = time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	for range 2 {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if err := s.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}
}

func testBatch() Batch {
	return Batch{
		Source: "test",
		Spans: []Span{
			{
				ID: "root", RunID: "r1", Kind: KindAgent, Name: "agent_loop",
				StartedAt: t0, EndedAt: t0.Add(3 * time.Second), Status: "ok",
			},
			{
				ID: "llm1", RunID: "r1", ParentID: "root", Kind: KindLLM, Name: "chat",
				StartedAt: t0.Add(time.Second), EndedAt: t0.Add(2 * time.Second),
				TokensIn: 100, TokensOut: 20, Status: "ok",
				Attrs: Attrs{Model: "fake-gpt", Provider: "fake"},
			},
			{
				ID: "tool1", RunID: "r1", ParentID: "root", Kind: KindTool, Name: "search",
				StartedAt: t0.Add(2 * time.Second), EndedAt: t0.Add(3 * time.Second),
				Status: "ok", Attrs: Attrs{ToolName: "search"},
			},
		},
		Contents: []Content{
			{SpanID: "llm1", Role: "user", Seq: 0, Body: "hi", MediaType: "text/plain"},
			{SpanID: "llm1", Role: "assistant", Seq: 1, Body: `{"a":1}`, MediaType: "application/json"},
		},
	}
}

func TestWriteBatchAggregatesRun(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.WriteBatch(ctx, testBatch()); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	runs, err := s.ListRuns(ctx)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("got %d runs, want 1", len(runs))
	}
	r := runs[0]
	if r.ID != "r1" || r.Source != "test" || r.Status != "ok" {
		t.Errorf("run = %+v", r)
	}
	if r.TokensIn != 100 || r.TokensOut != 20 {
		t.Errorf("tokens = %d/%d, want 100/20", r.TokensIn, r.TokensOut)
	}
	if r.ModelMain != "fake-gpt" {
		t.Errorf("model_main = %q, want fake-gpt", r.ModelMain)
	}
	if !r.StartedAt.Equal(t0) || !r.EndedAt.Equal(t0.Add(3*time.Second)) {
		t.Errorf("run times = %v..%v", r.StartedAt, r.EndedAt)
	}
	if r.CostUSD != nil {
		t.Errorf("cost = %v, want nil", *r.CostUSD)
	}
}

func TestWriteBatchRoundTripsSpansAndContents(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.WriteBatch(ctx, testBatch()); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	spans, err := s.Spans(ctx, "r1")
	if err != nil {
		t.Fatalf("Spans: %v", err)
	}
	if len(spans) != 3 {
		t.Fatalf("got %d spans, want 3", len(spans))
	}
	if spans[0].ID != "root" || spans[0].ParentID != "" || spans[0].Kind != KindAgent {
		t.Errorf("spans[0] = %+v", spans[0])
	}
	if spans[1].Attrs.Model != "fake-gpt" || spans[1].Attrs.Provider != "fake" {
		t.Errorf("spans[1].Attrs = %+v", spans[1].Attrs)
	}
	contents, err := s.Contents(ctx, "llm1")
	if err != nil {
		t.Fatalf("Contents: %v", err)
	}
	if len(contents) != 2 || contents[0].Body != "hi" || contents[1].MediaType != "application/json" {
		t.Errorf("contents = %+v", contents)
	}
}

func TestWriteBatchReplacesSpans(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	for range 2 {
		if err := s.WriteBatch(ctx, testBatch()); err != nil {
			t.Fatalf("WriteBatch: %v", err)
		}
	}
	spans, err := s.Spans(ctx, "r1")
	if err != nil {
		t.Fatalf("Spans: %v", err)
	}
	if len(spans) != 3 {
		t.Errorf("got %d spans after rewrite, want 3", len(spans))
	}
}

func TestRunStatusFollowsRootSpan(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	b := testBatch()
	b.Spans[2].Status = "error" // a failed tool call does not fail the run
	if err := s.WriteBatch(ctx, b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	runs, err := s.ListRuns(ctx)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if runs[0].Status != "ok" {
		t.Errorf("status with child error = %q, want ok", runs[0].Status)
	}
	b.Spans[0].Status = "error"
	if err := s.WriteBatch(ctx, b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	runs, err = s.ListRuns(ctx)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if runs[0].Status != "error" {
		t.Errorf("status with root error = %q, want error", runs[0].Status)
	}
}

func TestRunStatusSurfacesTerminalModelFailureAndRecovery(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	b := Batch{Source: "test", Spans: []Span{
		{ID: "task", RunID: "r1", Kind: KindOther, Name: "task",
			StartedAt: t0, EndedAt: t0.Add(5 * time.Second), Status: "ok"},
		{ID: "agent", RunID: "r1", ParentID: "task", Kind: KindAgent, Name: "agent",
			StartedAt: t0, EndedAt: t0.Add(4 * time.Second), Status: "ok"},
		{ID: "llm1", RunID: "r1", ParentID: "agent", Kind: KindLLM, Name: "chat",
			StartedAt: t0, EndedAt: t0.Add(3 * time.Second), Status: "error"},
	}}
	if err := s.WriteBatch(ctx, b); err != nil {
		t.Fatal(err)
	}
	runs, err := s.ListRuns(ctx)
	if err != nil || len(runs) != 1 || runs[0].Status != "error" {
		t.Fatalf("terminal model failure = %+v, %v", runs, err)
	}
	b.Spans = []Span{{ID: "llm2", RunID: "r1", ParentID: "agent",
		Kind: KindLLM, Name: "chat", StartedAt: t0.Add(3 * time.Second),
		EndedAt: t0.Add(4 * time.Second), Status: "ok"}}
	if err := s.WriteBatch(ctx, b); err != nil {
		t.Fatal(err)
	}
	runs, err = s.ListRuns(ctx)
	if err != nil || runs[0].Status != "ok" {
		t.Fatalf("recovered model failure = %+v, %v", runs, err)
	}
}

func TestDropContentRedactsRawAndPreviousBodies(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	b := Batch{Source: "test", Spans: []Span{{
		ID: "s1", RunID: "r1", Kind: KindLLM, Name: "chat",
		EndedAt: t0, Status: "ok",
		Attrs: Attrs{Raw: map[string]any{
			"gen_ai.input.messages":          "DEMO_SECRET",
			"gen_ai.usage.input_tokens":      100,
			"gen_ai.response.finish_reasons": []any{"MAX_OUTPUT_TOKENS"},
			"error.type":                     "DEMO_SECRET",
			"finish_reason":                  "DEMO_SECRET",
			"usage":                          map[string]any{"input_tokens": 100, "prompt": "DEMO_SECRET"},
		}},
	}}, Contents: []Content{{SpanID: "s1", Body: "DEMO_SECRET"}}}
	if err := s.WriteBatch(ctx, b); err != nil {
		t.Fatal(err)
	}
	b.DropContent = true
	if err := s.WriteBatch(ctx, b); err != nil {
		t.Fatal(err)
	}
	contents, err := s.Contents(ctx, "s1")
	if err != nil || len(contents) != 0 {
		t.Fatalf("contents after no-content reimport = %+v, %v", contents, err)
	}
	spans, err := s.Spans(ctx, "r1")
	if err != nil || len(spans) != 1 {
		t.Fatalf("spans = %+v, %v", spans, err)
	}
	raw := spans[0].Attrs.Raw
	if raw["gen_ai.input.messages"] != nil || raw["error.type"] != nil ||
		raw["finish_reason"] != nil || raw["gen_ai.usage.input_tokens"] != float64(100) {
		t.Fatalf("redacted attrs = %+v", raw)
	}
	if reasons, ok := raw["gen_ai.response.finish_reasons"].([]any); !ok ||
		len(reasons) != 1 || reasons[0] != "max_output_tokens" {
		t.Fatalf("finish reasons = %+v", raw["gen_ai.response.finish_reasons"])
	}
	usage, ok := raw["usage"].(map[string]any)
	if !ok || usage["prompt"] != nil || usage["input_tokens"] != float64(100) {
		t.Fatalf("redacted usage = %+v", raw["usage"])
	}
}

func TestDropContentKeepsInvalidCacheCountAsMissingPriceSignal(t *testing.T) {
	s := openTemp(t)
	sp := Span{ID: "s1", RunID: "r1", Kind: KindLLM, Name: "chat",
		EndedAt: t0, Status: "ok", Attrs: Attrs{Raw: map[string]any{
			"gen_ai.usage.cache_read.input_tokens": "DEMO_SECRET",
		}}}
	if err := s.WriteBatch(context.Background(), Batch{
		Source: "test", Spans: []Span{sp}, DropContent: true,
	}); err != nil {
		t.Fatal(err)
	}
	spans, err := s.Spans(context.Background(), "r1")
	if err != nil || len(spans) != 1 {
		t.Fatalf("spans = %+v, %v", spans, err)
	}
	raw := spans[0].Attrs.Raw
	if value, present := raw["gen_ai.usage.cache_read.input_tokens"]; !present || value != nil {
		t.Fatalf("invalid count marker = %+v", raw)
	}
}

func TestRunWithoutFinishedRootIsRunning(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	b := Batch{Source: "test", Spans: []Span{{
		ID: "llm1", RunID: "r1", ParentID: "root", Kind: KindLLM, Name: "chat",
		StartedAt: t0, EndedAt: t0.Add(time.Second), Status: "ok",
	}}}
	if err := s.WriteBatch(ctx, b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	runs, err := s.ListRuns(ctx)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if runs[0].Status != "running" {
		t.Errorf("status = %q, want running", runs[0].Status)
	}
}

func TestSubscribeSignalsOnWrite(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	ch, cancel := s.Subscribe()
	defer cancel()
	for range 3 {
		if err := s.WriteBatch(ctx, testBatch()); err != nil {
			t.Fatalf("WriteBatch: %v", err)
		}
	}
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("no signal after write")
	}
	cancel()
	if err := s.WriteBatch(ctx, testBatch()); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	// Cancel closes the channel; at most one buffered signal may precede that.
	for i := 0; ; i++ {
		if _, ok := <-ch; !ok {
			break
		}
		if i > 0 {
			t.Fatal("more than one signal after cancel")
		}
	}
}

func TestModelMainTieBreaksDeterministically(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	b := Batch{Source: "test", Spans: []Span{
		{
			ID: "l1", RunID: "r1", Kind: KindLLM, Name: "chat",
			StartedAt: t0, EndedAt: t0.Add(time.Second), Status: "ok",
			Attrs: Attrs{Model: "model-b"},
		},
		{
			ID: "l2", RunID: "r1", Kind: KindLLM, Name: "chat",
			StartedAt: t0, EndedAt: t0.Add(time.Second), Status: "ok",
			Attrs: Attrs{Model: "model-a"},
		},
	}}
	if err := s.WriteBatch(ctx, b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	runs, err := s.ListRuns(ctx)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if runs[0].ModelMain != "model-a" {
		t.Errorf("model_main = %q, want model-a on tie", runs[0].ModelMain)
	}
}

func TestFindingsDedupeAndCount(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	b := testBatch()
	b.Findings = []Finding{
		{RunID: "r1", Type: "parse_error", Severity: "warning", Detail: `{"line":3}`},
		{RunID: "r1", SpanID: "tool1", Type: "parse_error", Severity: "warning", Detail: `{"line":9}`},
	}
	for range 2 { // re-tailing a file re-emits identical findings
		if err := s.WriteBatch(ctx, b); err != nil {
			t.Fatalf("WriteBatch: %v", err)
		}
	}
	runs, err := s.ListRuns(ctx)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if runs[0].Findings != 2 {
		t.Errorf("findings count = %d, want 2", runs[0].Findings)
	}
	findings, err := s.Findings(ctx, "r1")
	if err != nil {
		t.Fatalf("Findings: %v", err)
	}
	if len(findings) != 2 || findings[0].SpanID != "" || findings[1].SpanID != "tool1" {
		t.Errorf("findings = %+v", findings)
	}
}

func TestFindingOnlyBatchCreatesRun(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	b := Batch{Source: "claude", Findings: []Finding{
		{RunID: "r9", Type: "parse_error", Severity: "warning", Detail: `{"line":1}`},
	}}
	if err := s.WriteBatch(ctx, b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	runs, err := s.ListRuns(ctx)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != "r9" || runs[0].Findings != 1 {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestSetRunLabel(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.SetRunLabel(ctx, "r1", "claude", "fix the login bug"); err != nil {
		t.Fatalf("SetRunLabel before spans: %v", err)
	}
	if err := s.WriteBatch(ctx, testBatch()); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	if err := s.SetRunLabel(ctx, "r1", "claude", "fix the login bug, again"); err != nil {
		t.Fatalf("SetRunLabel after spans: %v", err)
	}
	runs, err := s.ListRuns(ctx)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if runs[0].Label != "fix the login bug, again" {
		t.Errorf("label = %q", runs[0].Label)
	}
}

func TestToolSchemaVersions(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if ts, err := s.LatestToolSchema(ctx, "search"); err != nil || ts != nil {
		t.Fatalf("LatestToolSchema empty = %v, %v", ts, err)
	}
	v1 := ToolSchema{
		ToolName: "search", Version: 1, Schema: `{"type":["object"]}`,
		LearnedFromRun: "r1", FirstSeen: t0, LastSeen: t0,
	}
	if err := s.InsertToolSchema(ctx, v1); err != nil {
		t.Fatalf("InsertToolSchema: %v", err)
	}
	if err := s.TouchToolSchema(ctx, "search", 1, `{"type":["object","array"]}`, t0.Add(time.Hour)); err != nil {
		t.Fatalf("TouchToolSchema: %v", err)
	}
	v2 := v1
	v2.Version, v2.LearnedFromRun = 2, "r2"
	if err := s.InsertToolSchema(ctx, v2); err != nil {
		t.Fatalf("InsertToolSchema v2: %v", err)
	}
	latest, err := s.LatestToolSchema(ctx, "search")
	if err != nil {
		t.Fatalf("LatestToolSchema: %v", err)
	}
	if latest.Version != 2 || latest.LearnedFromRun != "r2" {
		t.Errorf("latest = %+v", latest)
	}
}

func TestResetAnalysisForgetsLearnedSchemas(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.WriteBatch(ctx, testBatch()); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	if err := s.InsertToolSchema(ctx, ToolSchema{
		ToolName: "search", Version: 1, Schema: `{"type":["object"]}`,
		LearnedFromRun: "r1", FirstSeen: t0, LastSeen: t0,
	}); err != nil {
		t.Fatalf("InsertToolSchema: %v", err)
	}
	pending, err := s.UnanalyzedSpans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkAnalyzed(ctx, pending); err != nil {
		t.Fatalf("MarkAnalyzed: %v", err)
	}
	if err := s.ResetAnalysis(ctx); err != nil {
		t.Fatalf("ResetAnalysis: %v", err)
	}
	if ts, err := s.LatestToolSchema(ctx, "search"); err != nil || ts != nil {
		t.Fatalf("schema survived reset: %v, %v", ts, err)
	}
	spans, err := s.UnanalyzedSpans(ctx)
	if err != nil {
		t.Fatalf("UnanalyzedSpans: %v", err)
	}
	if len(spans) != 3 {
		t.Fatalf("reset left %d spans analyzed, want all 3 unanalyzed", 3-len(spans))
	}
}

func TestUnanalyzedSpansLifecycle(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.WriteBatch(ctx, testBatch()); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	spans, err := s.UnanalyzedSpans(ctx)
	if err != nil {
		t.Fatalf("UnanalyzedSpans: %v", err)
	}
	if len(spans) != 3 {
		t.Fatalf("unanalyzed = %d, want 3", len(spans))
	}
	if !spans[0].EndedAt.Before(spans[2].EndedAt) {
		t.Errorf("not ordered by end time: %v, %v", spans[0].EndedAt, spans[2].EndedAt)
	}
	if err := s.MarkAnalyzed(ctx, spans); err != nil {
		t.Fatalf("MarkAnalyzed: %v", err)
	}
	spans, err = s.UnanalyzedSpans(ctx)
	if err != nil {
		t.Fatalf("UnanalyzedSpans: %v", err)
	}
	if len(spans) != 0 {
		t.Fatalf("unanalyzed after mark = %d, want 0", len(spans))
	}
	// A re-written span drops back into the queue for re-analysis.
	if err := s.WriteBatch(ctx, testBatch()); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	spans, err = s.UnanalyzedSpans(ctx)
	if err != nil {
		t.Fatalf("UnanalyzedSpans: %v", err)
	}
	if len(spans) != 3 {
		t.Fatalf("unanalyzed after rewrite = %d, want 3", len(spans))
	}
}

func TestParseKind(t *testing.T) {
	if k := ParseKind("llm"); k != KindLLM {
		t.Errorf("ParseKind(llm) = %q", k)
	}
	if k := ParseKind("bogus"); k != KindOther {
		t.Errorf("ParseKind(bogus) = %q", k)
	}
}
