package otlp

import (
	"testing"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
)

func TestToLogsRedactsAndKeepsIdentity(t *testing.T) {
	ld := plog.NewLogs()
	r := ld.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	r.SetTraceID(pcommon.TraceID{1})
	r.SetSpanID(pcommon.SpanID{2})
	r.Body().SetStr("secret")
	r.Attributes().PutStr("private", "value")
	full := ToLogs(ld, true)
	redacted := ToLogs(ld, false)
	if len(full) != 1 || full[0].Body != "secret" {
		t.Fatalf("full: %+v", full)
	}
	if len(redacted) != 1 || redacted[0].Body != "" || redacted[0].AttrsJSON != "{}" {
		t.Fatalf("redacted: %+v", redacted)
	}
	if full[0].RunID != redacted[0].RunID || full[0].SpanID != redacted[0].SpanID {
		t.Fatal("lost linkage")
	}
}
