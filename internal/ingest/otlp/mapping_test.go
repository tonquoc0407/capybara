package otlp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonquoc0407/capybara/internal/store"
)

func TestMappingTypesUnmappedSpan(t *testing.T) {
	td, span := singleSpan()
	span.Attributes().PutStr("myframework.span.type", "reranker")
	cfg := &Mapping{Kinds: []KindRule{
		{Attr: "myframework.span.type", Equals: "reranker", Kind: "retrieval"},
	}}
	b := toBatch(td, true, cfg)
	if b.Spans[0].Kind != store.KindRetrieval {
		t.Fatalf("kind = %q, want retrieval", b.Spans[0].Kind)
	}
}

func TestMappingFieldAndContentAliases(t *testing.T) {
	td, span := singleSpan()
	span.Attributes().PutStr("myframework.span.type", "chat")
	span.Attributes().PutStr("mf.model", "acme-1")
	span.Attributes().PutInt("mf.tokens.in", 12)
	span.Attributes().PutStr("mf.prompt", "hello")
	span.Attributes().PutStr("mf.response", "hi there")
	cfg := &Mapping{
		Kinds:   []KindRule{{Attr: "myframework.span.type", Equals: "chat", Kind: "llm"}},
		Fields:  FieldAliases{Model: []string{"mf.model"}, InputTokens: []string{"mf.tokens.in"}},
		Content: ContentAliases{Input: []string{"mf.prompt"}, Output: []string{"mf.response"}},
	}
	b := toBatch(td, true, cfg)
	sp := b.Spans[0]
	if sp.Kind != store.KindLLM || sp.Attrs.Model != "acme-1" || sp.TokensIn != 12 {
		t.Fatalf("span = %+v", sp)
	}
	roles := map[string]string{}
	for _, c := range b.Contents {
		roles[c.Role] = c.Body
	}
	if roles["user"] != "hello" || roles["assistant"] != "hi there" {
		t.Fatalf("contents = %+v", b.Contents)
	}
}

// A mapping fills gaps; it must never reclassify a span the built-ins already
// recognised, or a stray config could silently corrupt a known convention.
func TestMappingDoesNotOverrideBuiltins(t *testing.T) {
	td, span := singleSpan()
	span.Attributes().PutStr("gen_ai.operation.name", "chat") // built-in -> llm
	cfg := &Mapping{Kinds: []KindRule{
		{Attr: "gen_ai.operation.name", Equals: "chat", Kind: "tool"},
	}}
	if b := toBatch(td, true, cfg); b.Spans[0].Kind != store.KindLLM {
		t.Fatalf("kind = %q, want llm (built-in wins)", b.Spans[0].Kind)
	}
}

func TestLoadMappingAbsentInvalidAndValid(t *testing.T) {
	if m, err := LoadMapping(filepath.Join(t.TempDir(), "nope.toml")); m != nil || err != nil {
		t.Fatalf("absent file = %v, %v; want nil, nil", m, err)
	}
	bad := filepath.Join(t.TempDir(), "bad.toml")
	if err := os.WriteFile(bad, []byte("[[kind]]\nattr='x'\nkind='wizard'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadMapping(bad); err == nil {
		t.Fatal("unknown kind must error")
	}
	noAttr := filepath.Join(t.TempDir(), "noattr.toml")
	if err := os.WriteFile(noAttr, []byte("[[kind]]\nkind='tool'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadMapping(noAttr); err == nil {
		t.Fatal("missing attr must error")
	}
	good := filepath.Join(t.TempDir(), "good.toml")
	body := "[[kind]]\nattr='x'\nequals='y'\nkind='tool'\n[fields]\nmodel=['m']\n[content]\noutput=['o']\n"
	if err := os.WriteFile(good, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := LoadMapping(good)
	if err != nil || m == nil || len(m.Kinds) != 1 || m.Kinds[0].Kind != "tool" ||
		len(m.Fields.Model) != 1 || len(m.Content.Output) != 1 {
		t.Fatalf("good = %+v, %v", m, err)
	}
}

func TestReceiverLoadsAndSurfacesMapping(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfgDir := filepath.Join(dir, "capybara")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfgDir, "mapping.toml")

	if err := os.WriteFile(path, []byte("[[kind]]\nattr='x'\nkind='agent'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := New(nil, true); r.mapping == nil || r.mappingErr != nil || len(r.mapping.Kinds) != 1 {
		t.Fatalf("good mapping not loaded: %+v err=%v", r.mapping, r.mappingErr)
	}

	if err := os.WriteFile(path, []byte("[[kind]]\nattr='x'\nkind='bogus'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := New(nil, true)
	if r.mappingErr == nil {
		t.Fatal("bad mapping must set mappingErr")
	}
	// Listen surfaces it before binding any port.
	if err := r.Listen(); err == nil || !strings.Contains(err.Error(), "mapping") {
		t.Fatalf("Listen = %v, want a mapping error", err)
	}
}
