package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

// AlertConfig posts one event when a threshold becomes active. A new event is
// sent only after the condition clears and later crosses again.
type AlertConfig struct {
	Project       string
	WebhookURL    string
	Window        time.Duration
	Interval      time.Duration
	MinErrors     int64
	MaxP95Latency time.Duration
	MaxCostUSD    float64
}

// AlertEvent is the payload sent when an alert threshold becomes active.
type AlertEvent struct {
	Summary    store.AlertWindow `json:"summary"`
	Violations []string          `json:"violations"`
	At         string            `json:"at"`
}

type alertMonitor struct {
	store  *store.Store
	config AlertConfig
	client *http.Client
	active bool
}

func (c AlertConfig) validate() error {
	if c.WebhookURL == "" {
		return nil
	}
	if c.Project == "" || c.Window <= 0 || c.Interval <= 0 || c.MinErrors < 0 || c.MaxP95Latency < 0 || c.MaxCostUSD < 0 {
		return errors.New("invalid alert configuration")
	}
	if c.MinErrors == 0 && c.MaxP95Latency == 0 && c.MaxCostUSD == 0 {
		return errors.New("at least one alert threshold is required")
	}
	return nil
}

func (m *alertMonitor) check(ctx context.Context, now time.Time) (bool, error) {
	summary, err := m.store.RunAlertWindow(ctx, m.config.Project, now.Add(-m.config.Window))
	if err != nil {
		return false, err
	}
	var violations []string
	if m.config.MinErrors > 0 && summary.Errors >= m.config.MinErrors {
		violations = append(violations, "errors")
	}
	if m.config.MaxP95Latency > 0 && summary.P95LatencyMS > float64(m.config.MaxP95Latency)/float64(time.Millisecond) {
		violations = append(violations, "p95_latency")
	}
	if m.config.MaxCostUSD > 0 && summary.CostUSD > m.config.MaxCostUSD {
		violations = append(violations, "known_cost")
	}
	if len(violations) == 0 {
		m.active = false
		return false, nil
	}
	if m.active {
		return false, nil
	}
	event := AlertEvent{Summary: summary, Violations: violations, At: now.UTC().Format(time.RFC3339)}
	body, err := json.Marshal(event)
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.config.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("alert webhook returned %d", resp.StatusCode)
	}
	m.active = true
	if err := m.store.RecordAudit(ctx, "system", "alert.sent", m.config.Project, fmt.Sprint(violations)); err != nil {
		return true, err
	}
	return true, nil
}
