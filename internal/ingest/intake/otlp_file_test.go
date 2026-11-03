package intake

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

func otlpLine(t *testing.T) []byte {
	t.Helper()
	traces := ptrace.NewTraces()
	sp := traces.ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	sp.SetTraceID(pcommon.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	sp.SetSpanID(pcommon.SpanID{1, 2, 3, 4, 5, 6, 7, 8})
	sp.SetName("chat")
	at := pcommon.NewTimestampFromTime(time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC))
	sp.SetStartTimestamp(at)
	sp.SetEndTimestamp(at + 1)
	sp.Attributes().PutStr("gen_ai.operation.name", "chat")
	sp.Attributes().PutStr("gen_ai.request.model", "claude-sonnet-4-6")
	sp.Attributes().PutInt("gen_ai.usage.input_tokens", 100_000)
	sp.Attributes().PutInt("gen_ai.usage.cache_read.input_tokens", 80_000)
	sp.Attributes().PutStr("gen_ai.input.messages", "DEMO_SECRET")
	raw, err := (&ptrace.JSONMarshaler{}).MarshalTraces(traces)
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}

func TestImportOTLPJSONLNoContent(t *testing.T) {
	st := openTemp(t)
	if err := ImportOTLPJSONL(context.Background(), st, bytes.NewReader(otlpLine(t)), false); err != nil {
		t.Fatal(err)
	}
	runs, err := st.ListRuns(context.Background())
	if err != nil || len(runs) != 1 || runs[0].Source != "otlp-file" {
		t.Fatalf("runs = %+v, %v", runs, err)
	}
	spans, err := st.Spans(context.Background(), runs[0].ID)
	if err != nil || len(spans) != 1 || spans[0].TokensIn != 100_000 {
		t.Fatalf("spans = %+v, %v", spans, err)
	}
	if spans[0].Attrs.Raw["gen_ai.input.messages"] != nil ||
		spans[0].Attrs.Raw["gen_ai.usage.cache_read.input_tokens"] != float64(80_000) {
		t.Fatalf("raw attrs = %+v", spans[0].Attrs.Raw)
	}
}

func TestImportOTLPJSONLRejectsMixedFileAtomically(t *testing.T) {
	st := openTemp(t)
	data := append(otlpLine(t), []byte(`{"resourceLogs":[]}\n`)...)
	err := ImportOTLPJSONL(context.Background(), st, bytes.NewReader(data), true)
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("error = %v", err)
	}
	runs, err := st.ListRuns(context.Background())
	if err != nil || len(runs) != 0 {
		t.Fatalf("partial import = %+v, %v", runs, err)
	}
}
