package otlp

import (
	"context"
	"encoding/binary"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/tonquoc0407/capybara/internal/store"
)

func burstTraces(n int) ptrace.Traces {
	td := ptrace.NewTraces()
	spans := td.ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans()
	base := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		var traceID pcommon.TraceID
		binary.BigEndian.PutUint64(traceID[8:], uint64(i/100+1))
		var spanID pcommon.SpanID
		binary.BigEndian.PutUint64(spanID[:], uint64(i+1))
		span := spans.AppendEmpty()
		span.SetTraceID(traceID)
		span.SetSpanID(spanID)
		span.SetName("chat")
		span.SetStartTimestamp(pcommon.NewTimestampFromTime(base.Add(time.Duration(i) * time.Second)))
		span.SetEndTimestamp(pcommon.NewTimestampFromTime(base.Add(time.Duration(i)*time.Second + time.Millisecond)))
		span.Attributes().PutStr("gen_ai.operation.name", "chat")
		span.Attributes().PutStr("gen_ai.request.model", "gpt-4o")
		span.Attributes().PutInt("gen_ai.usage.input_tokens", 100)
		span.Attributes().PutInt("gen_ai.usage.output_tokens", 20)
	}
	return td
}

func BenchmarkBurstOTLP(b *testing.B) {
	for _, n := range []int{1000, 10000, 100000} {
		b.Run(fmt.Sprintf("spans-%d", n), func(b *testing.B) {
			td := burstTraces(n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				st, err := store.Open(filepath.Join(b.TempDir(), "trace.db"))
				if err != nil {
					b.Fatal(err)
				}
				r := New(st, true)
				b.StartTimer()
				err = r.ingest(context.Background(), td)
				b.StopTimer()
				if err != nil {
					_ = st.Close()
					b.Fatal(err)
				}
				if err := st.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
