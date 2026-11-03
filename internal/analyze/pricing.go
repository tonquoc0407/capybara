package analyze

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/tonquoc0407/capybara/internal/store"
)

//go:embed pricing.json
var embeddedPricing []byte

// rates are USD per million tokens. CacheWrite and CacheRead default to the
// input rate when a provider has no cache tiers.
type rates struct {
	Input      float64  `json:"input"`
	Output     float64  `json:"output"`
	CacheWrite *float64 `json:"cache_write"`
	CacheRead  *float64 `json:"cache_read"`
}

// pricing maps model-name prefixes to rates; the longest matching prefix wins.
// Unknown models get token counts, no dollar figure, no guessing.
type pricing map[string]rates

// loadPricing merges the user's override file over the embedded table.
func loadPricing(overridePath string) (pricing, error) {
	p := pricing{}
	if err := json.Unmarshal(embeddedPricing, &p); err != nil {
		return nil, fmt.Errorf("embedded pricing: %w", err)
	}
	raw, err := os.ReadFile(overridePath)
	if errors.Is(err, fs.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", overridePath, err)
	}
	override := pricing{}
	if err := json.Unmarshal(raw, &override); err != nil {
		return nil, fmt.Errorf("parse %s: %w", overridePath, err)
	}
	for model, r := range override {
		p[model] = r
	}
	return p, nil
}

// DefaultPricingPath returns ~/.config/capybara/pricing.json.
func DefaultPricingPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "capybara", "pricing.json")
}

func (p pricing) lookup(model string) (rates, bool) {
	// Google's SDKs report the full resource name, so a LangChain run through
	// Gemini arrives as models/gemini-2.5-flash and matches nothing.
	model = strings.TrimPrefix(model, "models/")
	best, found := "", false
	var r rates
	for prefix, pr := range p {
		if !strings.HasPrefix(model, prefix) || len(prefix) <= len(best) {
			continue
		}
		if !datedRelease(model[len(prefix):]) {
			continue
		}
		best, r, found = prefix, pr, true
	}
	return r, found
}

// datedRelease reports whether what follows a matched prefix is only a release
// stamp. Anything else is a different model that happens to share the prefix —
// claude-opus-4-8 must not inherit claude-opus-4 rates.
func datedRelease(suffix string) bool {
	if suffix == "" {
		return true
	}
	const stampLen = 7 // -YYYYMMDD and -YYYY-MM-DD both clear this
	if len(suffix) < stampLen || suffix[0] != '-' {
		return false
	}
	for _, c := range suffix[1:] {
		if (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

// spanCost prices one llm span, preferring the detailed usage breakdown that
// cache-aware ingests record in raw attrs.
func (p pricing) spanCost(sp store.Span) *float64 {
	if sp.Attrs.Model == "" {
		return nil
	}
	r, ok := p.lookup(sp.Attrs.Model)
	if !ok {
		return nil
	}
	perTok := func(rate float64) float64 { return rate / 1e6 }
	cacheWrite, cacheRead := r.Input, r.Input
	if r.CacheWrite != nil {
		cacheWrite = *r.CacheWrite
	}
	if r.CacheRead != nil {
		cacheRead = *r.CacheRead
	}
	if u, ok := sp.Attrs.Raw["usage"].(map[string]any); ok {
		if _, ok := u["input_tokens"]; !ok {
			return nil
		}
		if _, ok := u["output_tokens"]; !ok {
			return nil
		}
		input, inputOK := optionalCount(u, "input_tokens")
		output, outputOK := optionalCount(u, "output_tokens")
		write, writeOK := optionalCount(u, "cache_creation_input_tokens")
		read, readOK := optionalCount(u, "cache_read_input_tokens")
		if !inputOK || !outputOK || !writeOK || !readOK {
			return nil
		}
		cost := input*perTok(r.Input) + output*perTok(r.Output) +
			write*perTok(cacheWrite) + read*perTok(cacheRead)
		return &cost
	}
	// OpenTelemetry's input_tokens is the total, including cache reads and
	// writes. Pricing it all as fresh input overstates cached requests.
	if raw := sp.Attrs.Raw; raw != nil {
		_, hasRead := raw["gen_ai.usage.cache_read.input_tokens"]
		_, hasWrite := raw["gen_ai.usage.cache_creation.input_tokens"]
		if hasRead || hasWrite {
			read, readOK := optionalCount(raw, "gen_ai.usage.cache_read.input_tokens")
			write, writeOK := optionalCount(raw, "gen_ai.usage.cache_creation.input_tokens")
			total := float64(sp.TokensIn)
			if !readOK || !writeOK || total <= 0 || read+write > total {
				return nil // incomplete or contradictory usage is not a price
			}
			cost := (total-read-write)*perTok(r.Input) +
				read*perTok(cacheRead) + write*perTok(cacheWrite) +
				float64(sp.TokensOut)*perTok(r.Output)
			return &cost
		}
	}
	if sp.TokensIn == 0 && sp.TokensOut == 0 {
		return nil
	}
	cost := float64(sp.TokensIn)*perTok(r.Input) + float64(sp.TokensOut)*perTok(r.Output)
	return &cost
}

func optionalCount(fields map[string]any, key string) (float64, bool) {
	v, ok := fields[key]
	if !ok {
		return 0, true
	}
	return tokenCount(v)
}

func tokenCount(v any) (float64, bool) {
	var value float64
	switch n := v.(type) {
	case float64:
		value = n
	case int64:
		value = float64(n)
	case int:
		value = float64(n)
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return 0, false
		}
		value = f
	default:
		return 0, false
	}
	return value, !math.IsNaN(value) && !math.IsInf(value, 0) &&
		value >= 0 && math.Trunc(value) == value
}
