package intake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

// replayTrace is one agent-replay (github.com/clay-good/agent-replay) trace.
// Steps carry durations but no timestamps; span times stay NULL rather than
// being fabricated, with step metadata kept in raw attrs.
type replayTrace struct {
	AgentName    string          `json:"agent_name"`
	AgentVersion string          `json:"agent_version"`
	SessionID    string          `json:"session_id"`
	Trigger      string          `json:"trigger"`
	Status       string          `json:"status"`
	Input        json.RawMessage `json:"input"`
	Output       json.RawMessage `json:"output"`
	StartedAt    time.Time       `json:"started_at"`
	EndedAt      time.Time       `json:"ended_at"`
	TotalCostUSD *float64        `json:"total_cost_usd"`
	Error        json.RawMessage `json:"error"`
	Steps        []replayStep    `json:"steps"`
}

type replayStep struct {
	StepNumber   int             `json:"step_number"`
	StepType     string          `json:"step_type"`
	Name         string          `json:"name"`
	Input        json.RawMessage `json:"input"`
	Output       json.RawMessage `json:"output"`
	DurationMS   *float64        `json:"duration_ms"`
	TokensUsed   *float64        `json:"tokens_used"`
	ParentStep   *int            `json:"parent_step"`
	CausedByStep *int            `json:"caused_by_step"`
}

var kindByStepType = map[string]store.Kind{
	"llm_call":  store.KindLLM,
	"tool_call": store.KindTool,
	"retrieval": store.KindRetrieval,
}

// ImportReplay reads an agent-replay JSON export: one trace or an array of them.
func ImportReplay(ctx context.Context, st *store.Store, r io.Reader, captureContent bool) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	var traces []replayTrace
	if err := json.Unmarshal(data, &traces); err != nil {
		var one replayTrace
		if err := json.Unmarshal(data, &one); err != nil {
			return fmt.Errorf("not an agent-replay trace: %w", err)
		}
		traces = []replayTrace{one}
	}
	batch := store.Batch{Source: "agent-replay"}
	labels := make(map[string]string)
	for i, tr := range traces {
		if tr.AgentName == "" {
			return fmt.Errorf("trace %d: missing agent_name", i)
		}
		runID := tr.runID()
		labels[runID] = tr.AgentName
		mapReplayTrace(&batch, runID, tr, captureContent)
	}
	if err := st.WriteBatch(ctx, batch); err != nil {
		return fmt.Errorf("write batch: %w", err)
	}
	for runID, label := range labels {
		if err := st.SetRunLabel(ctx, runID, "agent-replay", label); err != nil {
			return err
		}
	}
	return nil
}

// runID prefers the export's own session id; otherwise the identity of a
// trace is its agent plus start time.
func (tr replayTrace) runID() string {
	if tr.SessionID != "" {
		return tr.SessionID
	}
	sum := sha256.Sum256([]byte(tr.AgentName + "|" + tr.StartedAt.Format(time.RFC3339Nano)))
	return "replay-" + hex.EncodeToString(sum[:8])
}

func mapReplayTrace(b *store.Batch, runID string, tr replayTrace, capture bool) {
	rootID := runID + ":root"
	status := "ok"
	if tr.Status == "failed" || tr.Status == "timeout" {
		status = "error"
	}
	b.Spans = append(b.Spans, store.Span{
		ID: rootID, RunID: runID, Kind: store.KindAgent, Name: tr.AgentName,
		StartedAt: tr.StartedAt, EndedAt: tr.EndedAt, CostUSD: tr.TotalCostUSD,
		Status: status,
		Attrs: store.Attrs{Raw: dropEmpty(map[string]any{
			"agent_version": tr.AgentVersion,
			"trigger":       tr.Trigger,
			"status":        tr.Status,
		})},
	})
	if capture {
		seq := 0
		seq = addRawContent(b, rootID, "input", tr.Input, seq)
		seq = addRawContent(b, rootID, "output", tr.Output, seq)
		addRawContent(b, rootID, "error", tr.Error, seq)
	}
	idByStep := make(map[int]string, len(tr.Steps))
	for _, step := range tr.Steps {
		idByStep[step.StepNumber] = fmt.Sprintf("%s:step:%06d", runID, step.StepNumber)
	}
	for _, step := range tr.Steps {
		id := idByStep[step.StepNumber]
		parent := rootID
		if step.ParentStep != nil {
			if pid, ok := idByStep[*step.ParentStep]; ok {
				parent = pid
			}
		}
		kind, ok := kindByStepType[step.StepType]
		if !ok {
			kind = store.KindOther
		}
		name := step.Name
		if name == "" {
			name = step.StepType
		}
		stepStatus := "ok"
		if step.StepType == "error" {
			stepStatus = "error"
		}
		sp := store.Span{
			ID: id, RunID: runID, ParentID: parent, Kind: kind, Name: name,
			Status: stepStatus,
			Attrs: store.Attrs{Raw: dropEmpty(map[string]any{
				"step_number":    step.StepNumber,
				"step_type":      step.StepType,
				"duration_ms":    step.DurationMS,
				"tokens_used":    step.TokensUsed,
				"caused_by_step": step.CausedByStep,
			})},
		}
		if kind == store.KindTool {
			sp.Attrs.ToolName = name
		}
		b.Spans = append(b.Spans, sp)
		if capture {
			seq := addRawContent(b, id, "input", step.Input, 0)
			addRawContent(b, id, "output", step.Output, seq)
		}
	}
}

func addRawContent(b *store.Batch, spanID, role string, raw json.RawMessage, seq int) int {
	if len(raw) == 0 || string(raw) == "null" {
		return seq
	}
	body := blockString(raw)
	mediaType := "text/plain"
	if json.Valid([]byte(body)) {
		mediaType = "application/json"
	}
	b.Contents = append(b.Contents, store.Content{
		SpanID: spanID, Role: role, Seq: seq, Body: body, MediaType: mediaType,
	})
	return seq + 1
}

// blockString unquotes plain JSON strings and passes objects through raw.
func blockString(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

func dropEmpty(m map[string]any) map[string]any {
	for k, v := range m {
		switch t := v.(type) {
		case string:
			if t == "" {
				delete(m, k)
			}
		case *float64:
			if t == nil {
				delete(m, k)
			}
		case *int:
			if t == nil {
				delete(m, k)
			}
		}
	}
	return m
}
