package otlp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"go.opentelemetry.io/collector/pdata/plog"

	"github.com/tonquoc0407/capybara/internal/store"
)

// ToLogs keeps only trace-linked records; general infrastructure logs belong
// in the user's existing log backend. IDs make retried exports idempotent.
func ToLogs(ld plog.Logs, captureContent bool) []store.TraceLog {
	var out []store.TraceLog
	resources := ld.ResourceLogs()
	for i := 0; i < resources.Len(); i++ {
		scopes := resources.At(i).ScopeLogs()
		for j := 0; j < scopes.Len(); j++ {
			records := scopes.At(j).LogRecords()
			for k := 0; k < records.Len(); k++ {
				r := records.At(k)
				if r.TraceID().IsEmpty() {
					continue
				}
				body := ""
				attrs := "{}"
				if captureContent {
					body = r.Body().AsString()
					if raw, err := json.Marshal(r.Attributes().AsRaw()); err == nil {
						attrs = string(raw)
					}
				}
				at := r.Timestamp()
				if at == 0 {
					at = r.ObservedTimestamp()
				}
				identity := r.TraceID().String() + "\x00" + r.SpanID().String() + "\x00" +
					at.AsTime().Format("2006-01-02T15:04:05.000000000Z07:00") + "\x00" +
					r.SeverityText() + "\x00" + r.Body().AsString() + "\x00" + attrs
				h := sha256.Sum256([]byte(identity))
				out = append(out, store.TraceLog{
					ID: hex.EncodeToString(h[:]), RunID: r.TraceID().String(),
					SpanID: r.SpanID().String(), ObservedAt: toTime(at),
					Severity: r.SeverityText(), Body: body, AttrsJSON: attrs,
				})
			}
		}
	}
	return out
}
