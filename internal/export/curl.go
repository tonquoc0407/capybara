package export

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tonquoc0407/capybara/internal/store"
)

// Curl builds a copy-pasteable curl command reproducing an LLM call from runID.
func Curl(ctx context.Context, st *store.Store, runID string) (string, error) {
	spans, err := st.Spans(ctx, runID)
	if err != nil {
		return "", err
	}
	var llmSpan *store.Span
	for i := range spans {
		if spans[i].Kind == store.KindLLM {
			llmSpan = &spans[i]
			break
		}
	}
	if llmSpan == nil {
		return "", fmt.Errorf("no llm calls found in run %s", runID)
	}
	contents, err := st.Contents(ctx, llmSpan.ID)
	if err != nil {
		return "", err
	}
	model := llmSpan.Attrs.Model
	provider := strings.ToLower(llmSpan.Attrs.Provider)
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
		return formatAnthropicCurl(model, contents)
	case "google":
		return formatGoogleCurl(model, contents)
	default:
		return formatOpenAICurl(model, contents)
	}
}

func formatOpenAICurl(model string, contents []store.Content) (string, error) {
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
		if role == "assistant" || role == "user" || role == "system" {
			messages = append(messages, msg{Role: role, Content: c.Body})
		} else if role == "prompt" || role == "input" {
			messages = append(messages, msg{Role: "user", Content: c.Body})
		}
	}
	payload := map[string]any{
		"model":    model,
		"messages": messages,
	}
	body, err := jsonIndent(payload)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("curl -s -X POST https://api.openai.com/v1/chat/completions \\\n"+
		"  -H \"Content-Type: application/json\" \\\n"+
		"  -H \"Authorization: Bearer $OPENAI_API_KEY\" \\\n"+
		"  -d '%s'", body), nil
}

func formatAnthropicCurl(model string, contents []store.Content) (string, error) {
	if model == "" {
		model = "claude-3-5-sonnet-20241022"
	}
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	var system string
	var messages []msg
	for _, c := range contents {
		if c.Role == "system" {
			system = c.Body
		} else if c.Role == "user" || c.Role == "prompt" || c.Role == "input" {
			messages = append(messages, msg{Role: "user", Content: c.Body})
		} else if c.Role == "assistant" {
			messages = append(messages, msg{Role: "assistant", Content: c.Body})
		}
	}
	payload := map[string]any{
		"model":      model,
		"max_tokens": 1024,
		"messages":   messages,
	}
	if system != "" {
		payload["system"] = system
	}
	body, err := jsonIndent(payload)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("curl -s -X POST https://api.anthropic.com/v1/messages \\\n"+
		"  -H \"Content-Type: application/json\" \\\n"+
		"  -H \"x-api-key: $ANTHROPIC_API_KEY\" \\\n"+
		"  -H \"anthropic-version: 2023-06-01\" \\\n"+
		"  -d '%s'", body), nil
}

func formatGoogleCurl(model string, contents []store.Content) (string, error) {
	if model == "" {
		model = "gemini-2.5-flash"
	}
	type part struct {
		Text string `json:"text"`
	}
	type contentObj struct {
		Role  string `json:"role,omitempty"`
		Parts []part `json:"parts"`
	}
	var cList []contentObj
	for _, c := range contents {
		role := c.Role
		if role == "assistant" {
			role = "model"
		} else if role == "prompt" || role == "input" {
			role = "user"
		}
		cList = append(cList, contentObj{
			Role:  role,
			Parts: []part{{Text: c.Body}},
		})
	}
	payload := map[string]any{"contents": cList}
	body, err := jsonIndent(payload)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("curl -s -X POST \"https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=$GEMINI_API_KEY\" \\\n"+
		"  -H \"Content-Type: application/json\" \\\n"+
		"  -d '%s'", model, body), nil
}

func jsonIndent(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}
