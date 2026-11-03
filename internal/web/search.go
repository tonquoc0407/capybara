package web

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

// ParseRunSearch keeps project ownership under server control in hub mode.
// An empty forcedProject permits local cross-project exploration.
func ParseRunSearch(r *http.Request, forcedProject string) (store.RunSearch, error) {
	q := r.URL.Query()
	filter := store.RunSearch{Project: q.Get("project"), Environment: q.Get("environment"),
		Release: q.Get("release"), Status: q.Get("status"), Query: q.Get("q"), Limit: 50}
	if forcedProject != "" {
		filter.Project = forcedProject
	}
	for _, part := range []struct {
		name string
		dst  *int
	}{{"limit", &filter.Limit}, {"offset", &filter.Offset}} {
		if raw := q.Get(part.name); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil {
				return filter, fmt.Errorf("invalid %s", part.name)
			}
			*part.dst = n
		}
	}
	for _, part := range []struct {
		name string
		dst  *time.Time
	}{{"from", &filter.From}, {"to", &filter.To}} {
		if raw := q.Get(part.name); raw != "" {
			at, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				return filter, fmt.Errorf("invalid %s; expected RFC3339", part.name)
			}
			*part.dst = at
		}
	}
	if !filter.From.IsZero() && !filter.To.IsZero() && filter.From.After(filter.To) {
		return filter, fmt.Errorf("from must be before to")
	}
	if filter.Limit < 1 || filter.Limit > 100 || filter.Offset < 0 {
		return filter, fmt.Errorf("limit must be 1..100 and offset non-negative")
	}
	return filter, nil
}
