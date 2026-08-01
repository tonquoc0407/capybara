package analyze

import (
	"context"
	"strings"
	"testing"
)

func TestFindSecret(t *testing.T) {
	cases := []struct {
		name, body, kind string
		want             bool
	}{
		{"aws key", "export AWS_KEY=AKIAIOSFODNN7EXAMPLE now", "aws access key", true},
		{"github token", "token ghp_0123456789abcdefghijklmnopqrstuvwxyz here", "github token", true},
		{"google key", "key=AIza0123456789abcdefghijklmnopqrstuvwxy end", "google api key", true},
		{"stripe live key", "sk_live_0123456789abcdefABCDEF ok", "stripe key", true},
		{"jwt", "auth eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w done", "jwt", true},
		{"private key", "-----BEGIN RSA PRIVATE KEY-----\nMIIabc", "private key", true},
		{"visa card, luhn ok", "card 4111 1111 1111 1111 on file", "card number", true},
		{"benign prose", "the api key documentation explains authentication", "", false},
		{"card fails luhn", "ref 4111 1111 1111 1112 ok", "", false},
		{"digits but not a card iin", "order 1234567890123452", "", false},
	}
	for _, c := range cases {
		kind, sample, ok := findSecret(c.body)
		if ok != c.want || kind != c.kind {
			t.Errorf("%s: findSecret = (%q, %v), want (%q, %v)", c.name, kind, ok, c.kind, c.want)
			continue
		}
		if ok && strings.Contains(c.body, sample) {
			t.Errorf("%s: sample %q is not masked, it appears verbatim in the body", c.name, sample)
		}
	}
}

func TestLuhn(t *testing.T) {
	if !luhn("4111111111111111") {
		t.Error("known-good Visa number failed Luhn")
	}
	if luhn("4111111111111112") {
		t.Error("altered number passed Luhn")
	}
}

func TestSecretLeakFlagsTheCarryingSpan(t *testing.T) {
	st := openTemp(t)
	b := improviseRunBatch("r1", "config dump: ghp_0123456789abcdefghijklmnopqrstuvwxyz", "Here is the config.", "ok")
	if err := st.WriteBatch(context.Background(), b); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	sweep(t, st)
	fs := runFindings(t, st, "r1")
	if len(fs) != 1 || fs[0].Type != "secret_leak" || fs[0].SpanID != "r1-tool" {
		t.Fatalf("findings = %+v", fs)
	}
	if !strings.Contains(fs[0].Detail, "github token") || strings.Contains(fs[0].Detail, "0123456789abcdef") {
		t.Errorf("detail leaks or mislabels the secret: %s", fs[0].Detail)
	}
}
