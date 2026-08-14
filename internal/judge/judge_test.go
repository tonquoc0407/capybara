package judge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type stub struct {
	reply string
	err   error
	got   string
}

func (s *stub) Complete(_ context.Context, _, user string) (string, error) {
	s.got = user
	return s.reply, s.err
}

func TestClientComplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"model":"m"`) {
			t.Errorf("body = %s", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": "hi"}}},
		})
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL + "/v1", Model: "m", Key: "k", HTTP: srv.Client()}
	out, err := c.Complete(context.Background(), "sys", "usr")
	if err != nil || out != "hi" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestClientCompleteHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": "slow down"}})
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Model: "m", HTTP: srv.Client()}
	if _, err := c.Complete(context.Background(), "s", "u"); err == nil || !strings.Contains(err.Error(), "slow down") {
		t.Fatalf("err = %v", err)
	}
}

func TestGradeFaithful(t *testing.T) {
	s := &stub{reply: `{"faithful": true, "unsupported": []}`}
	got, err := Grade(context.Background(), s, "the price is 42", []string{"price is 42"})
	if err != nil || got != nil {
		t.Fatalf("got=%v err=%v", got, err)
	}
	if !strings.Contains(s.got, "CONTEXT:") || !strings.Contains(s.got, "ANSWER:") {
		t.Errorf("prompt missing sections: %q", s.got)
	}
}

func TestGradeUnfaithful(t *testing.T) {
	s := &stub{reply: "Here is my verdict:\n```json\n{\"faithful\": false, \"unsupported\": [\"the price is 99\"]}\n```"}
	got, err := Grade(context.Background(), s, "the price is 99", []string{"price is 42"})
	if err != nil || len(got) != 1 || got[0] != "the price is 99" {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

func TestParseVerdictNoJSON(t *testing.T) {
	if _, err := parseVerdict("I cannot answer that."); err == nil {
		t.Fatal("expected error on reply with no json")
	}
}

func TestGradeToolsReturnsWrongIndices(t *testing.T) {
	s := &stub{reply: `{"wrong": [2]}`}
	got, err := GradeTools(context.Background(), s, "what is the weather?",
		[]ToolCall{{Name: "get_weather", Args: `{"city":"Oslo"}`}, {Name: "delete_account", Args: `{"id":7}`}})
	if err != nil || len(got) != 1 || got[0] != 2 {
		t.Fatalf("got=%v err=%v", got, err)
	}
	if !strings.Contains(s.got, "get_weather") || !strings.Contains(s.got, "2. delete_account") {
		t.Errorf("prompt = %q", s.got)
	}
}

func TestGradeToolsAllAppropriate(t *testing.T) {
	s := &stub{reply: `{"wrong": []}`}
	got, err := GradeTools(context.Background(), s, "q", []ToolCall{{Name: "search", Args: "{}"}})
	if err != nil || len(got) != 0 {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

func TestGradeRelevanceRelevant(t *testing.T) {
	s := &stub{reply: `{"relevant": true, "reason": ""}`}
	got, err := GradeRelevance(context.Background(), s, "what is the capital of France?", "Paris.")
	if err != nil || got != "" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if !strings.Contains(s.got, "REQUEST:") || !strings.Contains(s.got, "ANSWER:") {
		t.Errorf("prompt missing sections: %q", s.got)
	}
}

func TestGradeRelevanceOffTopic(t *testing.T) {
	s := &stub{reply: `{"relevant": false, "reason": "answers a different question"}`}
	got, err := GradeRelevance(context.Background(), s, "what is the capital of France?", "The weather is sunny today.")
	if err != nil || got != "answers a different question" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestGradeRelevanceNoJSON(t *testing.T) {
	s := &stub{reply: "I cannot answer that."}
	if _, err := GradeRelevance(context.Background(), s, "q", "a"); err == nil {
		t.Fatal("expected error on reply with no json")
	}
}
