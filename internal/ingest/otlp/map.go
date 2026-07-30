package otlp

import (
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/tonquoc0407/capybara/internal/store"
)

// ToBatch maps OTLP traces onto a store batch, one run per trace id.
// Unrecognized spans map to kind other; nothing is dropped.
func ToBatch(td ptrace.Traces, captureContent bool) store.Batch {
	return toBatch(td, captureContent, nil)
}

func toBatch(td ptrace.Traces, captureContent bool, cfg *Mapping) store.Batch {
	b := store.Batch{Source: "otlp"}
	resources := td.ResourceSpans()
	for i := 0; i < resources.Len(); i++ {
		scopes := resources.At(i).ScopeSpans()
		for j := 0; j < scopes.Len(); j++ {
			spans := scopes.At(j).Spans()
			for k := 0; k < spans.Len(); k++ {
				span := spans.At(k)
				b.Spans = append(b.Spans, toSpan(span, cfg))
				if captureContent {
					b.Contents = append(b.Contents, spanContents(span, cfg)...)
				}
			}
		}
	}
	return b
}

func toSpan(span ptrace.Span, cfg *Mapping) store.Span {
	m := mapSemconv(span, cfg)
	m.attrs.Raw = span.Attributes().AsRaw()
	parent := ""
	if !span.ParentSpanID().IsEmpty() {
		parent = span.ParentSpanID().String()
	}
	status := "ok"
	if span.Status().Code() == ptrace.StatusCodeError {
		status = "error"
	}
	return store.Span{
		ID:        span.SpanID().String(),
		RunID:     span.TraceID().String(),
		ParentID:  parent,
		Kind:      m.kind,
		Name:      span.Name(),
		StartedAt: toTime(span.StartTimestamp()),
		EndedAt:   toTime(span.EndTimestamp()),
		TokensIn:  m.tokensIn,
		TokensOut: m.tokensOut,
		Status:    status,
		Attrs:     m.attrs,
	}
}

// toTime keeps unset OTLP timestamps as the zero time instead of the unix epoch.
func toTime(ts pcommon.Timestamp) time.Time {
	if ts == 0 {
		return time.Time{}
	}
	return ts.AsTime()
}
