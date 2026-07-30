// Package judge grades RAG faithfulness with an external LLM over an
// OpenAI-compatible endpoint. It is the opt-in, non-deterministic half of the
// check: nothing here runs unless a command is given an endpoint, and the
// answer and documents it sends leave the machine, so the caller decides.
package judge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Completer returns a model's reply to a system and user message.
type Completer interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

// Client calls an OpenAI-compatible /chat/completions endpoint. BaseURL is the
// api root, e.g. http://localhost:11434/v1 for Ollama; Key may be empty for a
// local server.
type Client struct {
	BaseURL string
	Model   string
	Key     string
	HTTP    *http.Client
}

// Complete posts the two messages and returns the model's reply text.
func (c *Client) Complete(ctx context.Context, system, user string) (string, error) {
	body, err := json.Marshal(chatRequest{
		Model: c.Model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	})
	if err != nil {
		return "", err
	}
	url := strings.TrimRight(c.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Key != "" {
		req.Header.Set("Authorization", "Bearer "+c.Key)
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode judge reply: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("judge http %d: %s", resp.StatusCode, out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("judge returned no choices")
	}
	return out.Choices[0].Message.Content, nil
}

const gradeSystem = "You are a strict faithfulness grader for a retrieval system. " +
	"Given CONTEXT documents and an ANSWER, decide whether every factual claim in " +
	"the ANSWER is supported by the CONTEXT. Reply with ONLY a JSON object: " +
	`{"faithful": true|false, "unsupported": ["<claim>", ...]}. ` +
	"List a claim only when the CONTEXT does not support it."

// Grade returns the answer's claims the judge found unsupported by the docs;
// empty means faithful.
func Grade(ctx context.Context, c Completer, answer string, docs []string) ([]string, error) {
	user := "CONTEXT:\n" + strings.Join(docs, "\n---\n") + "\n\nANSWER:\n" + answer
	reply, err := c.Complete(ctx, gradeSystem, user)
	if err != nil {
		return nil, err
	}
	verdict, err := parseVerdict(reply)
	if err != nil {
		return nil, err
	}
	if verdict.Faithful {
		return nil, nil
	}
	return verdict.Unsupported, nil
}

type verdict struct {
	Faithful    bool     `json:"faithful"`
	Unsupported []string `json:"unsupported"`
}

// parseVerdict pulls the JSON object out of a reply that may be fenced or padded
// with prose, which local models in particular tend to add.
func parseVerdict(reply string) (verdict, error) {
	obj, err := extractJSON(reply)
	if err != nil {
		return verdict{}, err
	}
	var v verdict
	if err := json.Unmarshal([]byte(obj), &v); err != nil {
		return verdict{}, fmt.Errorf("parse judge verdict: %w", err)
	}
	return v, nil
}

// ToolCall is one tool the agent invoked, as the judge should see it.
type ToolCall struct {
	Name string
	Args string
}

const toolSystem = "You review an agent's tool use. Given the USER REQUEST and the " +
	"numbered TOOL CALLS the agent made, decide which calls were the wrong tool for " +
	"the request or irrelevant to it. A call is wrong only when it does not help " +
	"answer the request. Reply with ONLY a JSON object: {\"wrong\": [<call number>, ...]}."

// GradeTools returns the 1-based positions of the tool calls the judge found
// wrong for the request; empty means every call was appropriate.
func GradeTools(ctx context.Context, c Completer, request string, calls []ToolCall) ([]int, error) {
	var b strings.Builder
	b.WriteString("USER REQUEST:\n")
	b.WriteString(request)
	b.WriteString("\n\nTOOL CALLS:\n")
	for i, tc := range calls {
		fmt.Fprintf(&b, "%d. %s(%s)\n", i+1, tc.Name, tc.Args)
	}
	reply, err := c.Complete(ctx, toolSystem, b.String())
	if err != nil {
		return nil, err
	}
	obj, err := extractJSON(reply)
	if err != nil {
		return nil, err
	}
	var w struct {
		Wrong []int `json:"wrong"`
	}
	if err := json.Unmarshal([]byte(obj), &w); err != nil {
		return nil, fmt.Errorf("parse tool verdict: %w", err)
	}
	return w.Wrong, nil
}

func extractJSON(reply string) (string, error) {
	start := strings.IndexByte(reply, '{')
	end := strings.LastIndexByte(reply, '}')
	if start < 0 || end < start {
		return "", fmt.Errorf("no json object in judge reply: %q", trunc(reply))
	}
	return reply[start : end+1], nil
}

func trunc(s string) string {
	if len(s) > 80 {
		return s[:80]
	}
	return s
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}
