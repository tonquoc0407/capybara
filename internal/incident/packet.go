package incident

import (
	"context"
	"fmt"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/export"
	"github.com/tonquoc0407/capybara/internal/store"
)

// Packet keeps observations separate from detector hypotheses and states which
// data is missing for a cost or replay conclusion.
type Packet struct {
	RunID                string                 `json:"run_id"`
	Fingerprint          string                 `json:"fingerprint"`
	Context              RunContext             `json:"context"`
	Observed             Observed               `json:"observed"`
	Hypotheses           []store.Finding        `json:"hypotheses"`
	Costs                []store.CostProvenance `json:"costs"`
	Scores               []store.Score          `json:"scores"`
	Fixture              *export.Fixture        `json:"fixture,omitempty"`
	Comparison           *Comparison            `json:"comparison,omitempty"`
	Missing              []string               `json:"missing"`
	ReplayInputsRecorded bool                   `json:"replay_inputs_recorded"`
}

// RunContext records the run metadata needed to interpret an incident.
type RunContext struct {
	Project     string   `json:"project"`
	Environment string   `json:"environment"`
	Release     string   `json:"release"`
	SessionID   string   `json:"session_id"`
	Status      string   `json:"status"`
	CostUSD     *float64 `json:"cost_usd,omitempty"`
}

// Comparison records observed differences from a parent replay run.
type Comparison struct {
	ParentRunID     string   `json:"parent_run_id"`
	ParentStatus    string   `json:"parent_status"`
	CurrentStatus   string   `json:"current_status"`
	FirstDivergence int      `json:"first_divergence"`
	CostDeltaUSD    *float64 `json:"cost_delta_usd,omitempty"`
}

// Observed holds trace evidence separately from detector findings.
type Observed struct {
	Spans    []store.Span               `json:"spans"`
	Logs     []store.TraceLog           `json:"logs"`
	Contents map[string][]store.Content `json:"contents"`
}

// BuildPacket gathers evidence, hypotheses, and known gaps for one run.
func BuildPacket(ctx context.Context, st *store.Store, runID string) (Packet, error) {
	run, err := st.GetRun(ctx, runID)
	if err != nil {
		return Packet{}, err
	}
	spans, err := st.Spans(ctx, runID)
	if err != nil {
		return Packet{}, err
	}
	if len(spans) == 0 {
		return Packet{}, fmt.Errorf("run %s has no spans", runID)
	}
	findings, err := st.Findings(ctx, runID)
	if err != nil {
		return Packet{}, err
	}
	logs, err := st.TraceLogs(ctx, runID)
	if err != nil {
		return Packet{}, err
	}
	contents, err := st.ContentsForRun(ctx, runID)
	if err != nil {
		return Packet{}, err
	}
	cached, err := st.LLMCache(ctx, runID)
	if err != nil {
		return Packet{}, err
	}
	costs, err := st.CostProvenanceForRun(ctx, runID)
	if err != nil {
		return Packet{}, err
	}
	scores, err := st.ListScores(ctx, runID)
	if err != nil {
		return Packet{}, err
	}
	cachedBySpan := map[string]bool{}
	for _, c := range cached {
		cachedBySpan[c.SpanID] = true
	}
	fp, _ := Fingerprint(spans, findings)
	p := Packet{
		RunID: runID, Fingerprint: fp,
		Context: RunContext{
			Project: run.Project, Environment: run.Environment, Release: run.Release,
			SessionID: run.SessionID, Status: run.Status, CostUSD: run.CostUSD,
		},
		Observed:   Observed{Spans: spans, Logs: logs, Contents: contents},
		Hypotheses: findings, Costs: costs, Scores: scores,
	}
	if fixture, err := export.BuildFixture(ctx, st, runID); err == nil {
		p.Fixture = &fixture
	} else {
		p.Missing = append(p.Missing, "fixture:"+err.Error())
	}
	if run.ParentRunID != "" {
		parent, err := st.GetRun(ctx, run.ParentRunID)
		if err == nil {
			diff, err := analyze.DiffRuns(ctx, st, run.ParentRunID, runID)
			if err == nil {
				comparison := &Comparison{
					ParentRunID: parent.ID, ParentStatus: parent.Status,
					CurrentStatus: run.Status, FirstDivergence: diff.FirstDivergence,
				}
				if parent.CostUSD != nil && run.CostUSD != nil {
					delta := *run.CostUSD - *parent.CostUSD
					comparison.CostDeltaUSD = &delta
				}
				p.Comparison = comparison
			} else {
				p.Missing = append(p.Missing, "comparison:"+err.Error())
			}
		} else {
			p.Missing = append(p.Missing, "parent_run")
		}
	}
	hasEntrypoint := false
	completeRecording := true
	for _, sp := range spans {
		if _, ok := sp.Attrs.Raw["capybara.entrypoint"].(string); ok {
			hasEntrypoint = true
		}
		if sp.Kind == store.KindLLM {
			if sp.TokensIn == 0 && sp.TokensOut == 0 {
				p.Missing = append(p.Missing, "usage:"+sp.ID)
			}
			if sp.CostUSD == nil {
				p.Missing = append(p.Missing, "cost:"+sp.ID)
			}
			if !cachedBySpan[sp.ID] {
				p.Missing = append(p.Missing, "recorded_llm:"+sp.ID)
				completeRecording = false
			}
		}
		if sp.Kind == store.KindTool {
			input, output := false, false
			for _, c := range contents[sp.ID] {
				if c.Role == "input" {
					input = true
				}
				if c.Role == "output" {
					output = true
				}
			}
			if !input || !output {
				p.Missing = append(p.Missing, "recorded_tool:"+sp.ID)
				completeRecording = false
			}
		}
	}
	p.ReplayInputsRecorded = hasEntrypoint && completeRecording
	if !hasEntrypoint {
		p.Missing = append(p.Missing, "replay_entrypoint")
	}
	return p, nil
}
