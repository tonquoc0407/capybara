// Package replay re-executes a recorded run against its own recording:
// cached model responses, recorded tool outputs, and an edited value at the
// span the user chose.
package replay

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// The hashes below are recomputed by the SDK runner from live call arguments,
// so both sides must serialize identically: arrays keep order, HTML escaping
// is off, and nothing is reparsed. capybara/_replay.py mirrors this file.

// HashLLMRequest identifies a model call by its model and message texts.
func HashLLMRequest(model string, messages []Message) string {
	canonical := make([]any, 0, 2)
	pairs := make([][2]string, 0, len(messages))
	for _, m := range messages {
		pairs = append(pairs, [2]string{m.Role, MessageText(m.Body)})
	}
	canonical = append(canonical, model, pairs)
	return sum(marshal(canonical))
}

// HashToolCall identifies a tool call by name and its recorded argument text,
// which the SDK serialized and the runner reproduces verbatim.
func HashToolCall(tool, arguments string) string {
	return sum([]byte(tool + "\x00" + arguments))
}

// Message is one recorded conversation turn.
type Message struct {
	Role string
	Body string
}

// MessageText reduces a recorded message body to the plain text a live call
// would carry: instrumentors serialize parts as JSON, plain prompts as text.
func MessageText(body string) string {
	var value any
	dec := json.NewDecoder(strings.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return strings.TrimSpace(body)
	}
	return strings.TrimSpace(partsText(value))
}

func partsText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		var b strings.Builder
		for _, el := range v {
			b.WriteString(partsText(el))
		}
		return b.String()
	case map[string]any:
		for _, key := range []string{"content", "text"} {
			if inner, ok := v[key]; ok {
				return partsText(inner)
			}
		}
	}
	return ""
}

func marshal(v any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil
	}
	return bytes.TrimRight(buf.Bytes(), "\n")
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
