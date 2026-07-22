package replay

import (
	"encoding/json"
	"os"
	"testing"
)

// The same fixture is asserted by sdk/tests/test_replay.py: if these hashes
// move, every recorded run becomes unreplayable by the SDK runner.
type hashFixture struct {
	LLM []struct {
		Model    string `json:"model"`
		Messages []struct {
			Role string `json:"role"`
			Body string `json:"body"`
		} `json:"messages"`
		Hash string `json:"hash"`
	} `json:"llm"`
	Tool []struct {
		Tool      string `json:"tool"`
		Arguments string `json:"arguments"`
		Hash      string `json:"hash"`
	} `json:"tool"`
}

func TestHashesMatchFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/hashes.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture hashFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for i, c := range fixture.LLM {
		messages := make([]Message, 0, len(c.Messages))
		for _, m := range c.Messages {
			messages = append(messages, Message{Role: m.Role, Body: m.Body})
		}
		if got := HashLLMRequest(c.Model, messages); got != c.Hash {
			t.Errorf("llm case %d: got %s, fixture %s", i, got, c.Hash)
		}
	}
	for i, c := range fixture.Tool {
		if got := HashToolCall(c.Tool, c.Arguments); got != c.Hash {
			t.Errorf("tool case %d: got %s, fixture %s", i, got, c.Hash)
		}
	}
}

func TestMessageTextUnwrapsRecordedParts(t *testing.T) {
	cases := map[string]string{
		`[{"type": "text", "content": "hello"}]`:    "hello",
		`[{"type":"text","text":"a"},{"text":"b"}]`: "ab",
		`  plain prompt  `:                          "plain prompt",
		`{"content": [{"text": " nested "}]}`:       "nested",
		`42`:                                        "",
	}
	for body, want := range cases {
		if got := MessageText(body); got != want {
			t.Errorf("MessageText(%q) = %q, want %q", body, got, want)
		}
	}
}

// A different argument string is a different call, so the replay must not
// serve the recorded output for it.
func TestToolHashSeparatesNameFromArguments(t *testing.T) {
	if HashToolCall("ab", "c") == HashToolCall("a", "bc") {
		t.Fatal("tool name and arguments are not separated")
	}
}
