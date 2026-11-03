package otlp

import (
	"testing"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

func TestToBatchCarriesRunContext(t *testing.T) {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("capybara.project", "checkout")
	rs.Resource().Attributes().PutStr("service.version", "v3")
	sp := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	sp.SetTraceID(pcommon.TraceID{1})
	sp.SetSpanID(pcommon.SpanID{2})
	sp.Attributes().PutStr("deployment.environment.name", "prod")
	sp.Attributes().PutStr("session.id", "session-7")
	b := ToBatch(td, false)
	meta := b.RunContexts[sp.TraceID().String()]
	if meta.Project != "checkout" || meta.Environment != "prod" || meta.Release != "v3" || meta.SessionID != "session-7" {
		t.Fatalf("context: %+v", meta)
	}
}
