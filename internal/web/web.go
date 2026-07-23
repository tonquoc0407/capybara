// Package web renders capybara's read-only view: the same page whether it is
// served from a database or exported with one run inlined.
package web

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"time"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/store"
)

//go:embed assets
var assets embed.FS

var page = template.Must(template.New("index").Parse(asset("assets/index.html")))

// RunSummary is one row of the run list.
type RunSummary struct {
	ID        string   `json:"id"`
	Source    string   `json:"source"`
	Label     string   `json:"label"`
	Model     string   `json:"model"`
	Status    string   `json:"status"`
	Started   string   `json:"started"`
	Duration  float64  `json:"duration"`
	TokensIn  int64    `json:"tokens_in"`
	TokensOut int64    `json:"tokens_out"`
	Cost      *float64 `json:"cost,omitempty"`
	Findings  int64    `json:"findings"`
}

// RunDetail is everything the page draws for one run.
type RunDetail struct {
	ID       string               `json:"id"`
	Spans    []SpanView           `json:"spans"`
	Findings []FindingView        `json:"findings"`
	Contents map[string][]Content `json:"contents"`
}

// SpanView is one node of the trace tree.
type SpanView struct {
	ID        string   `json:"id"`
	Parent    string   `json:"parent"`
	Kind      string   `json:"kind"`
	Name      string   `json:"name"`
	Tool      string   `json:"tool,omitempty"`
	Model     string   `json:"model,omitempty"`
	Status    string   `json:"status"`
	Duration  float64  `json:"duration"`
	TokensIn  int64    `json:"tokens_in"`
	TokensOut int64    `json:"tokens_out"`
	Cost      *float64 `json:"cost,omitempty"`
}

// FindingView carries the shared one-line wording plus the raw detail, so the
// page can expand it without a second implementation of the summaries.
type FindingView struct {
	Span     string `json:"span"`
	Type     string `json:"type"`
	Severity string `json:"severity"`
	Summary  string `json:"summary"`
	Detail   string `json:"detail,omitempty"`
}

// Content is one recorded prompt, completion or tool payload.
type Content struct {
	Role  string `json:"role"`
	Body  string `json:"body"`
	Media string `json:"media,omitempty"`
}

// Runs lists every run, newest first, as the page shows them.
func Runs(ctx context.Context, st *store.Store) ([]RunSummary, error) {
	runs, err := st.ListRuns(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]RunSummary, 0, len(runs))
	for _, r := range runs {
		out = append(out, RunSummary{
			ID: r.ID, Source: r.Source, Label: r.Label, Model: r.ModelMain,
			Status: r.Status, Started: stamp(r.StartedAt),
			Duration: seconds(r.StartedAt, r.EndedAt),
			TokensIn: r.TokensIn, TokensOut: r.TokensOut,
			Cost: r.CostUSD, Findings: r.Findings,
		})
	}
	return out, nil
}

// Run gathers one run's spans, findings and content.
func Run(ctx context.Context, st *store.Store, runID string) (RunDetail, error) {
	spans, err := st.Spans(ctx, runID)
	if err != nil {
		return RunDetail{}, err
	}
	findings, err := st.Findings(ctx, runID)
	if err != nil {
		return RunDetail{}, err
	}
	contents, err := st.ContentsForRun(ctx, runID)
	if err != nil {
		return RunDetail{}, err
	}
	d := RunDetail{ID: runID, Contents: map[string][]Content{}}
	for _, sp := range spans {
		d.Spans = append(d.Spans, SpanView{
			ID: sp.ID, Parent: sp.ParentID, Kind: string(sp.Kind), Name: sp.Name,
			Tool: sp.Attrs.ToolName, Model: sp.Attrs.Model, Status: sp.Status,
			Duration: seconds(sp.StartedAt, sp.EndedAt),
			TokensIn: sp.TokensIn, TokensOut: sp.TokensOut, Cost: sp.CostUSD,
		})
	}
	for _, f := range findings {
		d.Findings = append(d.Findings, FindingView{
			Span: f.SpanID, Type: f.Type, Severity: f.Severity,
			Summary: analyze.FindingSummary(f), Detail: f.Detail,
		})
	}
	for spanID, rows := range contents {
		for _, c := range rows {
			d.Contents[spanID] = append(d.Contents[spanID], Content{
				Role: c.Role, Body: c.Body, Media: c.MediaType,
			})
		}
	}
	return d, nil
}

// Page renders the view. A nil payload leaves the page to fetch its own data,
// which is what the served copy does.
func Page(payload any) ([]byte, error) {
	data := template.JS("null")
	if payload != nil {
		// encoding/json escapes <, > and & by default, so the result cannot
		// close the script element it sits in.
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("inline data: %w", err)
		}
		data = template.JS(raw)
	}
	var b bytes.Buffer
	err := page.Execute(&b, struct {
		CSS  template.CSS
		JS   template.JS
		Data template.JS
	}{
		CSS:  template.CSS(asset("assets/app.css")),
		JS:   template.JS(asset("assets/app.js")),
		Data: data,
	})
	if err != nil {
		return nil, fmt.Errorf("render page: %w", err)
	}
	return b.Bytes(), nil
}

// WriteHTML exports one run as a page that needs no server.
func WriteHTML(ctx context.Context, st *store.Store, runID, dir string) (string, error) {
	runs, err := Runs(ctx, st)
	if err != nil {
		return "", err
	}
	detail, err := Run(ctx, st, runID)
	if err != nil {
		return "", err
	}
	for _, r := range runs {
		if r.ID == runID {
			runs = []RunSummary{r}
			break
		}
	}
	body, err := Page(struct {
		Runs []RunSummary `json:"runs"`
		Run  RunDetail    `json:"run"`
	}{Runs: runs, Run: detail})
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	path := filepath.Join(dir, shortID(runID)+".html")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
}

func asset(name string) string {
	b, err := assets.ReadFile(name)
	if err != nil {
		panic(err) // the files are embedded; a miss is a build error
	}
	return string(b)
}

func seconds(start, end time.Time) float64 {
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return 0
	}
	return end.Sub(start).Seconds()
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
