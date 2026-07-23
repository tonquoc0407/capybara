package analyze

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
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
		cost := num(u["input_tokens"])*perTok(r.Input) +
			num(u["output_tokens"])*perTok(r.Output) +
			num(u["cache_creation_input_tokens"])*perTok(cacheWrite) +
			num(u["cache_read_input_tokens"])*perTok(cacheRead)
		return &cost
	}
	if sp.TokensIn == 0 && sp.TokensOut == 0 {
		return nil
	}
	cost := float64(sp.TokensIn)*perTok(r.Input) + float64(sp.TokensOut)*perTok(r.Output)
	return &cost
}

func num(v any) float64 {
	f, _ := v.(float64)
	return f
}
