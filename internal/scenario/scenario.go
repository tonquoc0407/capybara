// Package scenario runs a bounded, explicit JSON subprocess protocol for
// offline agent simulations. User code is not sandboxed by this package.
package scenario

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// Spec declares the agent command, test turns, mock tools, and execution limits.
type Spec struct {
	Name       string                     `json:"name"`
	Command    []string                   `json:"command"`
	Turns      []Turn                     `json:"turns"`
	ToolMocks  map[string]json.RawMessage `json:"tool_mocks"`
	TimeoutMS  int64                      `json:"timeout_ms"`
	MaxSteps   int                        `json:"max_steps"`
	MaxCostUSD float64                    `json:"max_cost_usd"`
}

// Turn supplies one user input and an optional expected answer fragment.
type Turn struct {
	User           string `json:"user"`
	ExpectContains string `json:"expect_contains"`
}

// Message is one entry in the conversation sent to the agent process.
type Message struct {
	Role    string          `json:"role"`
	Name    string          `json:"name,omitempty"`
	Content string          `json:"content,omitempty"`
	JSON    json.RawMessage `json:"json,omitempty"`
}

// Request is the JSON object written to the agent process on stdin.
type Request struct {
	Messages []Message `json:"messages"`
	Tools    []string  `json:"tools"`
}

// ToolCall names a declared mock tool and its JSON arguments.
type ToolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// Response is the JSON object read from the agent process on stdout.
type Response struct {
	Assistant string     `json:"assistant"`
	ToolCalls []ToolCall `json:"tool_calls"`
	CostUSD   *float64   `json:"cost_usd"`
}

// TurnResult records the observed answer and the turn's assertion result.
type TurnResult struct {
	User      string `json:"user"`
	Assistant string `json:"assistant"`
	Passed    bool   `json:"passed"`
	Reason    string `json:"reason,omitempty"`
}

// Result records the outcome and agent-reported cost of a scenario run.
type Result struct {
	Name            string       `json:"name"`
	Passed          bool         `json:"passed"`
	ReportedCostUSD float64      `json:"reported_cost_usd"`
	CostReported    bool         `json:"cost_reported"`
	Steps           int          `json:"steps"`
	Turns           []TurnResult `json:"turns"`
}

func (s *Spec) defaultsAndValidate() error {
	if len(s.Command) == 0 || strings.TrimSpace(s.Command[0]) == "" || len(s.Turns) == 0 {
		return errors.New("scenario needs a command and at least one turn")
	}
	if s.TimeoutMS == 0 {
		s.TimeoutMS = 30000
	}
	if s.MaxSteps == 0 {
		s.MaxSteps = 20
	}
	if s.TimeoutMS < 1 || s.MaxSteps < 1 || math.IsNaN(s.MaxCostUSD) || math.IsInf(s.MaxCostUSD, 0) || s.MaxCostUSD <= 0 {
		return errors.New("invalid scenario limit")
	}
	for _, turn := range s.Turns {
		if strings.TrimSpace(turn.User) == "" {
			return errors.New("every turn needs user text")
		}
	}
	return nil
}

// Validate applies default limits and rejects incomplete or invalid specs.
func (s *Spec) Validate() error { return s.defaultsAndValidate() }

// Run invokes a fresh process for each step with the full conversation on
// stdin. It uses only declared mock tool responses; no tool code is executed.
// The agent process itself still has the host's normal permissions.
func Run(parent context.Context, spec Spec) (Result, error) {
	if err := spec.defaultsAndValidate(); err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(spec.TimeoutMS)*time.Millisecond)
	defer cancel()
	result := Result{Name: spec.Name, Passed: true, Turns: make([]TurnResult, 0, len(spec.Turns))}
	var messages []Message
	tools := make([]string, 0, len(spec.ToolMocks))
	for name := range spec.ToolMocks {
		tools = append(tools, name)
	}
	sort.Strings(tools)
	for _, turn := range spec.Turns {
		messages = append(messages, Message{Role: "user", Content: turn.User})
		out := TurnResult{User: turn.User}
		for {
			if result.Steps >= spec.MaxSteps {
				out.Reason = "step limit reached"
				break
			}
			if err := ctx.Err(); err != nil {
				out.Reason = "time limit reached"
				break
			}
			response, err := call(ctx, spec.Command, Request{Messages: messages, Tools: tools})
			result.Steps++
			if err != nil {
				out.Reason = err.Error()
				break
			}
			if response.CostUSD == nil && spec.MaxCostUSD > 0 {
				out.Reason = "agent did not report cost; budget cannot be checked"
				break
			}
			if response.CostUSD != nil {
				result.CostReported = true
				if math.IsNaN(*response.CostUSD) || math.IsInf(*response.CostUSD, 0) || *response.CostUSD < 0 {
					out.Reason = "invalid reported cost"
					break
				}
				result.ReportedCostUSD += *response.CostUSD
				if spec.MaxCostUSD > 0 && result.ReportedCostUSD > spec.MaxCostUSD {
					out.Reason = "reported cost budget exceeded"
					break
				}
			}
			if len(response.ToolCalls) > 0 {
				messages = append(messages, Message{Role: "assistant", Content: response.Assistant})
				for _, call := range response.ToolCalls {
					mock, ok := spec.ToolMocks[call.Name]
					if !ok {
						out.Reason = "unmocked tool: " + call.Name
						break
					}
					messages = append(messages, Message{Role: "tool", Name: call.Name, JSON: mock})
				}
				if out.Reason != "" {
					break
				}
				continue
			}
			if response.Assistant == "" {
				out.Reason = "agent returned neither answer nor tool call"
				break
			}
			out.Assistant = response.Assistant
			messages = append(messages, Message{Role: "assistant", Content: response.Assistant})
			if turn.ExpectContains != "" && !strings.Contains(response.Assistant, turn.ExpectContains) {
				out.Reason = "answer did not contain expected text"
			} else {
				out.Passed = true
			}
			break
		}
		result.Turns = append(result.Turns, out)
		if !out.Passed {
			result.Passed = false
			break
		}
	}
	return result, nil
}

func call(ctx context.Context, argv []string, request Request) (Response, error) {
	input, err := json.Marshal(request)
	if err != nil {
		return Response{}, err
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin = bytes.NewReader(input)
	stdout, stderr := &limitedBuffer{max: 1 << 20}, &limitedBuffer{max: 1 << 16}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
		return Response{}, fmt.Errorf("agent command: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var response Response
	decoder := json.NewDecoder(stdout)
	if err := decoder.Decode(&response); err != nil {
		return Response{}, fmt.Errorf("agent response: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Response{}, errors.New("agent response must be exactly one JSON value")
	}
	return response, nil
}

type limitedBuffer struct {
	bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		return 0, errors.New("agent output limit reached")
	}
	return b.Buffer.Write(p)
}
