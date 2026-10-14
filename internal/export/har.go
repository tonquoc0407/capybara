package export

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/tonquoc0407/capybara/internal/store"
)

// HARRoot represents the top-level HTTP Archive 1.2 container.
type HARRoot struct {
	Log HARLog `json:"log"`
}

// HARLog represents the log object in a HAR export.
type HARLog struct {
	Version string     `json:"version"`
	Creator HARCreator `json:"creator"`
	Entries []HAREntry `json:"entries"`
}

// HARCreator identifies the software creating the HAR file.
type HARCreator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// HAREntry represents an individual HTTP transaction.
type HAREntry struct {
	StartedDateTime string      `json:"startedDateTime"`
	Time            int64       `json:"time"`
	Request         HARRequest  `json:"request"`
	Response        HARResponse `json:"response"`
	Cache           HARCache    `json:"cache"`
	Timings         HARTimings  `json:"timings"`
}

// HARRequest details an HTTP request.
type HARRequest struct {
	Method      string       `json:"method"`
	URL         string       `json:"url"`
	HTTPVersion string       `json:"httpVersion"`
	Headers     []HARHeader  `json:"headers"`
	QueryString []HARQuery   `json:"queryString"`
	Cookies     []HARCookie  `json:"cookies"`
	HeadersSize int          `json:"headersSize"`
	BodySize    int          `json:"bodySize"`
	PostData    *HARPostData `json:"postData,omitempty"`
}

// HARResponse details an HTTP response.
type HARResponse struct {
	Status      int         `json:"status"`
	StatusText  string      `json:"statusText"`
	HTTPVersion string      `json:"httpVersion"`
	Headers     []HARHeader `json:"headers"`
	Cookies     []HARCookie `json:"cookies"`
	Content     HARContent  `json:"content"`
	RedirectURL string      `json:"redirectURL"`
	HeadersSize int         `json:"headersSize"`
	BodySize    int         `json:"bodySize"`
}

// HARHeader represents an HTTP header.
type HARHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// HARQuery represents a query parameter.
type HARQuery struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// HARCookie represents an HTTP cookie.
type HARCookie struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// HARPostData represents posted data in a request.
type HARPostData struct {
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}

// HARContent represents the content of a response.
type HARContent struct {
	Size     int    `json:"size"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}

// HARCache represents cache information (empty for API traces).
type HARCache struct{}

// HARTimings breaks down request/response latency.
type HARTimings struct {
	Send    int64 `json:"send"`
	Wait    int64 `json:"wait"`
	Receive int64 `json:"receive"`
}

// BuildHAR constructs a standard HAR 1.2 representation of LLM calls in a run.
func BuildHAR(ctx context.Context, st *store.Store, runID string) (HARRoot, error) {
	spans, err := st.Spans(ctx, runID)
	if err != nil {
		return HARRoot{}, err
	}
	var entries []HAREntry
	for _, sp := range spans {
		if sp.Kind != store.KindLLM {
			continue
		}
		contents, err := st.Contents(ctx, sp.ID)
		if err != nil {
			return HARRoot{}, err
		}
		entries = append(entries, buildHAREntry(sp, contents))
	}
	if entries == nil {
		entries = []HAREntry{}
	}
	return HARRoot{
		Log: HARLog{
			Version: "1.2",
			Creator: HARCreator{Name: "capybara", Version: "dev"},
			Entries: entries,
		},
	}, nil
}

// WriteHAR writes a run's LLM calls to a .har file in dir and returns the written path.
func WriteHAR(ctx context.Context, st *store.Store, runID, dir string) (string, error) {
	root, err := BuildHAR(ctx, st, runID)
	if err != nil {
		return "", err
	}
	if err := ensureDir(dir); err != nil {
		return "", err
	}
	path := filepath.Join(dir, shortID(runID)+".har")
	raw, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal har %s: %w", path, err)
	}
	if err := writeFile(path, string(raw)+"\n"); err != nil {
		return "", err
	}
	return path, nil
}

func buildHAREntry(sp store.Span, contents []store.Content) HAREntry {
	started := sp.StartedAt.UTC().Format("2006-01-02T15:04:05.000Z")
	dur := sp.EndedAt.Sub(sp.StartedAt).Milliseconds()
	if dur < 0 {
		dur = 0
	}
	url := endpointForSpan(sp)
	reqText := requestPayloadForSpan(sp, contents)
	respText := responsePayloadForSpan(contents)

	status := 200
	statusText := "OK"
	if sp.Status == "error" {
		status = 500
		statusText = "Internal Server Error"
	}

	return HAREntry{
		StartedDateTime: started,
		Time:            dur,
		Request: HARRequest{
			Method:      "POST",
			URL:         url,
			HTTPVersion: "HTTP/1.1",
			Headers: []HARHeader{
				{Name: "Content-Type", Value: "application/json"},
				{Name: "Accept", Value: "application/json"},
			},
			QueryString: []HARQuery{},
			Cookies:     []HARCookie{},
			HeadersSize: -1,
			BodySize:    len(reqText),
			PostData: &HARPostData{
				MimeType: "application/json",
				Text:     reqText,
			},
		},
		Response: HARResponse{
			Status:      status,
			StatusText:  statusText,
			HTTPVersion: "HTTP/1.1",
			Headers: []HARHeader{
				{Name: "Content-Type", Value: "application/json"},
			},
			Cookies: []HARCookie{},
			Content: HARContent{
				Size:     len(respText),
				MimeType: "application/json",
				Text:     respText,
			},
			RedirectURL: "",
			HeadersSize: -1,
			BodySize:    len(respText),
		},
		Cache: HARCache{},
		Timings: HARTimings{
			Send:    0,
			Wait:    dur,
			Receive: 0,
		},
	}
}

func endpointForSpan(sp store.Span) string {
	if sp.Attrs.Raw != nil {
		if ep, ok := sp.Attrs.Raw["endpoint"].(string); ok && ep != "" {
			return ep
		}
		if ep, ok := sp.Attrs.Raw["url"].(string); ok && ep != "" {
			return ep
		}
	}
	model := sp.Attrs.Model
	provider := strings.ToLower(sp.Attrs.Provider)
	if provider == "" {
		switch {
		case strings.Contains(model, "claude"):
			provider = "anthropic"
		case strings.Contains(model, "gemini"):
			provider = "google"
		default:
			provider = "openai"
		}
	}
	switch provider {
	case "anthropic":
		return "https://api.anthropic.com/v1/messages"
	case "google":
		if model == "" {
			model = "gemini-2.5-flash"
		}
		return fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", model)
	default:
		return "https://api.openai.com/v1/chat/completions"
	}
}

func requestPayloadForSpan(sp store.Span, contents []store.Content) string {
	for _, c := range contents {
		if (c.Role == "prompt" || c.Role == "input") && json.Valid([]byte(c.Body)) {
			var obj map[string]any
			if err := json.Unmarshal([]byte(c.Body), &obj); err == nil {
				if _, ok := obj["messages"]; ok {
					return c.Body
				}
				if _, ok := obj["contents"]; ok {
					return c.Body
				}
			}
		}
	}
	model := sp.Attrs.Model
	if model == "" {
		model = "gpt-4o"
	}
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	var messages []msg
	for _, c := range contents {
		role := c.Role
		switch role {
		case "assistant", "user", "system":
			messages = append(messages, msg{Role: role, Content: c.Body})
		case "prompt", "input":
			messages = append(messages, msg{Role: "user", Content: c.Body})
		}
	}
	payload := map[string]any{
		"model":    model,
		"messages": messages,
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err == nil {
		return strings.TrimSpace(buf.String())
	}
	return "{}"
}

func responsePayloadForSpan(contents []store.Content) string {
	for _, c := range contents {
		if c.Role == "completion" || c.Role == "output" || c.Role == "assistant" {
			return c.Body
		}
	}
	return "{}"
}
