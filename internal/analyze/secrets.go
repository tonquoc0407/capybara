package analyze

import (
	"context"
	"regexp"

	"github.com/tonquoc0407/capybara/internal/store"
)

// Secret detection flags a credential or card number sitting in a span's
// recorded content, where it has already been sent to a model or logged by the
// trace. It is deliberately narrow: only tokens a provider issues with a fixed
// prefix, plus card numbers a Luhn check confirms. High-entropy guessing and
// bare emails or phone numbers are left out - they are common and benign in
// agent traffic, and flagging them would drown the signal that a real key
// leaked. The matched value is never stored; only its kind and a masked head go
// into the finding, so the detector does not leak what it caught.
var secretPatterns = []struct {
	kind string
	re   *regexp.Regexp
}{
	{"private key", regexp.MustCompile(`-----BEGIN (?:RSA |EC |DSA |OPENSSH |PGP )?PRIVATE KEY-----`)},
	{"aws access key", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{"github token", regexp.MustCompile(`\bgh[pousr]_[0-9A-Za-z]{36}\b`)},
	{"github pat", regexp.MustCompile(`\bgithub_pat_[0-9A-Za-z_]{60,}\b`)},
	{"google api key", regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{35}\b`)},
	{"slack token", regexp.MustCompile(`\bxox[baprs]-[0-9A-Za-z-]{10,}\b`)},
	{"stripe key", regexp.MustCompile(`\b[sr]k_live_[0-9A-Za-z]{16,}\b`)},
	{"openai key", regexp.MustCompile(`\bsk-(?:proj-)?[A-Za-z0-9_-]{32,}\b`)},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`)},
}

var cardCandidate = regexp.MustCompile(`\b\d[\d -]{11,21}\d\b`)

const secretScanLimit = 1 << 20

func (a *Analyzer) secretRun(ctx context.Context, rc *runContext) ([]store.Finding, error) {
	var findings []store.Finding
	for _, sp := range rc.spans {
		if !rc.fresh[sp.ID] {
			continue
		}
		cs, err := a.st.Contents(ctx, sp.ID)
		if err != nil {
			return nil, err
		}
		for _, c := range cs {
			if kind, sample, ok := findSecret(c.Body); ok {
				findings = append(findings, finding(sp, "secret_leak", "warning", map[string]any{
					"kind": kind, "evidence": sample,
				}))
				break
			}
		}
	}
	return findings, nil
}

func findSecret(body string) (kind, sample string, ok bool) {
	if len(body) > secretScanLimit {
		body = body[:secretScanLimit]
	}
	for _, p := range secretPatterns {
		if m := p.re.FindString(body); m != "" {
			return p.kind, maskSecret(m), true
		}
	}
	if m, found := findCard(body); found {
		return "card number", maskSecret(m), true
	}
	return "", "", false
}

// maskSecret keeps the head that names the kind - a provider prefix, a card's
// issuer digits - and drops the rest, so the finding identifies the leak
// without repeating it.
func maskSecret(s string) string {
	r := []rune(s)
	if len(r) <= 6 {
		return "…"
	}
	return string(r[:6]) + "…"
}

func findCard(body string) (string, bool) {
	for _, m := range cardCandidate.FindAllString(body, -1) {
		digits := onlyDigits(m)
		n := len(digits)
		if n < 13 || n > 19 || digits[0] < '3' || digits[0] > '6' {
			continue
		}
		if luhn(digits) {
			return m, true
		}
	}
	return "", false
}

func onlyDigits(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			out = append(out, s[i])
		}
	}
	return string(out)
}

func luhn(digits string) bool {
	sum := 0
	double := false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if double {
			if d *= 2; d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}
