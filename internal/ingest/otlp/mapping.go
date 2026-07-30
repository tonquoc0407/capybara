package otlp

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/tonquoc0407/capybara/internal/store"
)

// Mapping extends the built-in conventions without a rebuild: a user drops a
// TOML file naming the attributes their instrumentor emits, and the mapper
// consults it wherever the built-ins came up empty. A nil Mapping is the plain
// built-in behaviour, so an absent file changes nothing.
type Mapping struct {
	Kinds   []KindRule     `toml:"kind"`
	Fields  FieldAliases   `toml:"fields"`
	Content ContentAliases `toml:"content"`
}

// KindRule types a span the built-ins left as other: when Attr is present - and
// equals Equals, when that is set - the span takes Kind.
type KindRule struct {
	Attr   string `toml:"attr"`
	Equals string `toml:"equals"`
	Kind   string `toml:"kind"`
}

// FieldAliases are extra attribute keys to read a value from, appended after the
// built-in keys so a convention's own names resolve the model, provider, tool
// and token counts.
type FieldAliases struct {
	Model        []string `toml:"model"`
	Provider     []string `toml:"provider"`
	Tool         []string `toml:"tool"`
	InputTokens  []string `toml:"input_tokens"`
	OutputTokens []string `toml:"output_tokens"`
}

// ContentAliases are attribute keys holding the prompt and completion (or a
// tool's input and output) text.
type ContentAliases struct {
	Input  []string `toml:"input"`
	Output []string `toml:"output"`
}

var knownKinds = map[string]store.Kind{
	"agent":     store.KindAgent,
	"llm":       store.KindLLM,
	"tool":      store.KindTool,
	"retrieval": store.KindRetrieval,
	"other":     store.KindOther,
}

// LoadMapping reads a mapping file. A missing file is not an error - it means
// the built-in conventions stand alone.
func LoadMapping(path string) (*Mapping, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var m Mapping
	if err := toml.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for i, r := range m.Kinds {
		if r.Attr == "" {
			return nil, fmt.Errorf("%s: kind rule %d has no attr", path, i)
		}
		if _, ok := knownKinds[r.Kind]; !ok {
			return nil, fmt.Errorf("%s: kind rule %d has unknown kind %q", path, i, r.Kind)
		}
	}
	return &m, nil
}

// DefaultMappingPath returns ~/.config/capybara/mapping.toml.
func DefaultMappingPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "capybara", "mapping.toml")
}

// kind returns the configured kind for a span, or ("", false) when no rule fits.
func (m *Mapping) kind(attrs pcommon.Map) (store.Kind, bool) {
	if m == nil {
		return "", false
	}
	for _, r := range m.Kinds {
		v, ok := attrs.Get(r.Attr)
		if !ok || (r.Equals != "" && v.AsString() != r.Equals) {
			continue
		}
		return knownKinds[r.Kind], true
	}
	return "", false
}

func withAliases(builtin, extra []string) []string {
	if len(extra) == 0 {
		return builtin
	}
	return append(append([]string{}, builtin...), extra...)
}
