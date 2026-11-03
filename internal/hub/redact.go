package hub

import (
	"encoding/json"

	"github.com/tonquoc0407/capybara/internal/store"
)

// Redaction happens before any content or raw attribute reaches the database.
// It is deliberately a literal replacement within each string value so JSON
// stays valid even if a pattern matches quotes or punctuation.
func (s Server) redactString(value string) string {
	if s.RedactPattern == nil {
		return value
	}
	return s.RedactPattern.ReplaceAllString(value, "[REDACTED]")
}

func (s Server) redactValue(value any) any {
	switch value := value.(type) {
	case string:
		return s.redactString(value)
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, item := range value {
			out[key] = s.redactValue(item)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = s.redactValue(item)
		}
		return out
	default:
		return value
	}
}

func (s Server) redactBatch(batch *store.Batch) {
	if s.RedactPattern == nil {
		return
	}
	for i := range batch.Spans {
		sp := &batch.Spans[i]
		sp.Name = s.redactString(sp.Name)
		sp.Attrs.Model = s.redactString(sp.Attrs.Model)
		sp.Attrs.Provider = s.redactString(sp.Attrs.Provider)
		sp.Attrs.ToolName = s.redactString(sp.Attrs.ToolName)
		if sp.Attrs.Raw != nil {
			sp.Attrs.Raw = s.redactValue(sp.Attrs.Raw).(map[string]any)
		}
	}
	for i := range batch.Contents {
		batch.Contents[i].Body = s.redactString(batch.Contents[i].Body)
	}
	for i := range batch.Findings {
		batch.Findings[i].Detail = s.redactString(batch.Findings[i].Detail)
	}
	for id, meta := range batch.RunContexts {
		meta.Environment = s.redactString(meta.Environment)
		meta.Release = s.redactString(meta.Release)
		meta.SessionID = s.redactString(meta.SessionID)
		meta.UserID = s.redactString(meta.UserID)
		batch.RunContexts[id] = meta
	}
}

func (s Server) redactLogs(logs []store.TraceLog) {
	if s.RedactPattern == nil {
		return
	}
	for i := range logs {
		logs[i].Body = s.redactString(logs[i].Body)
		var attrs any
		if err := json.Unmarshal([]byte(logs[i].AttrsJSON), &attrs); err == nil {
			if raw, err := json.Marshal(s.redactValue(attrs)); err == nil {
				logs[i].AttrsJSON = string(raw)
			}
		}
	}
}
