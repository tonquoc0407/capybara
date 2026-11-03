package store

import (
	"encoding/json"
	"math"
	"strings"
)

// metadataOnly keeps values used by cost and error analysis when content
// capture is off. An allowlist is necessary because instrumentors can put
// prompts, tool results and credentials under arbitrary attribute names.
func metadataOnly(raw map[string]any) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]any)
	for key, value := range raw {
		if key == "usage" {
			if usage := numericUsage(value); len(usage) > 0 {
				out[key] = usage
			}
			continue
		}
		switch key {
		case "gen_ai.usage.input_tokens", "gen_ai.usage.output_tokens",
			"gen_ai.usage.prompt_tokens", "gen_ai.usage.completion_tokens",
			"gen_ai.usage.cache_read.input_tokens",
			"gen_ai.usage.cache_creation.input_tokens",
			"llm.token_count.prompt", "llm.token_count.completion",
			"ai.usage.promptTokens", "ai.usage.completionTokens",
			"http.response.status_code":
			if n, ok := safeNumber(value); ok {
				out[key] = n
			} else {
				// Preserve the fact that a reported count was invalid. Pricing
				// must not treat it as an absent cache field and guess a price.
				out[key] = nil
			}
		case "finish_reason", "stop_reason", "gen_ai.response.finish_reasons":
			if v, ok := safeFinishReason(value); ok {
				out[key] = v
			}
		}
	}
	return out
}

func numericUsage(value any) map[string]any {
	// Claude Code gives us a struct; generic JSONL and OTLP give us maps.
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var fields map[string]any
	if json.Unmarshal(data, &fields) != nil {
		return nil
	}
	out := make(map[string]any)
	for _, key := range []string{
		"input_tokens", "output_tokens",
		"cache_creation_input_tokens", "cache_read_input_tokens",
	} {
		if n, ok := safeNumber(fields[key]); ok {
			out[key] = n
		} else if _, present := fields[key]; present {
			out[key] = nil
		}
	}
	return out
}

func safeNumber(value any) (float64, bool) {
	var n float64
	switch v := value.(type) {
	case int:
		n = float64(v)
	case int64:
		n = float64(v)
	case float64:
		n = v
	case json.Number:
		var err error
		n, err = v.Float64()
		if err != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	return n, !math.IsNaN(n) && !math.IsInf(n, 0) && n >= 0 && math.Trunc(n) == n
}

func safeFinishReason(value any) (any, bool) {
	switch v := value.(type) {
	case string:
		reason := strings.ToLower(v)
		switch reason {
		case "stop", "length", "max_tokens", "maxtokens", "max_output_tokens",
			"model_length", "end_turn", "tool_use",
			"tool_calls", "stop_sequence", "content_filter":
			return reason, true
		}
		return nil, false
	case []any:
		if len(v) > 8 {
			return nil, false
		}
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := safeFinishReason(item)
			if !ok {
				return nil, false
			}
			out = append(out, s.(string))
		}
		return out, true
	}
	return nil, false
}
