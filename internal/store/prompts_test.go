package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"
)

func TestPromptVersionsResolveLateTraceReference(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "prompts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	content := "Answer with evidence."
	h := sha256.Sum256([]byte(content))
	sha := hex.EncodeToString(h[:])
	if err := st.WriteBatch(ctx, Batch{
		Source: "otlp", RunContexts: map[string]RunContext{"run": {Project: "checkout"}},
		Spans: []Span{{
			ID: "span", RunID: "run", Kind: KindLLM, Name: "chat", Status: "ok",
			Attrs: Attrs{Raw: map[string]any{"capybara.prompt.sha256": sha}},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	refs, err := st.PromptReferencesForRun(ctx, "run")
	if err != nil || len(refs) != 1 || refs[0].Prompt != nil {
		t.Fatalf("unregistered: %+v, %v", refs, err)
	}
	p1, err := st.RegisterPrompt(ctx, "checkout", "system", content, "system.txt")
	if err != nil || p1.Version != 1 {
		t.Fatalf("v1: %+v, %v", p1, err)
	}
	duplicate, err := st.RegisterPrompt(ctx, "checkout", "system", content, "other.txt")
	if err != nil || duplicate.ID != p1.ID {
		t.Fatalf("duplicate: %+v, %v", duplicate, err)
	}
	p2, err := st.RegisterPrompt(ctx, "checkout", "system", "Answer briefly.", "system.txt")
	if err != nil || p2.Version != 2 {
		t.Fatalf("v2: %+v, %v", p2, err)
	}
	refs, err = st.PromptReferencesForRun(ctx, "run")
	if err != nil || len(refs) != 1 || refs[0].Prompt == nil || refs[0].Prompt.ID != p1.ID {
		t.Fatalf("resolved: %+v, %v", refs, err)
	}
	versions, err := st.ListPromptVersions(ctx, "checkout", "system")
	if err != nil || len(versions) != 2 || versions[0].Version != 2 {
		t.Fatalf("versions: %+v, %v", versions, err)
	}
	if versions[0].Content != "" {
		t.Fatal("prompt body loaded in list")
	}
	opened, err := st.PromptVersion(ctx, versions[0].ID)
	if err != nil || opened.Content != "Answer briefly." {
		t.Fatalf("on-demand prompt content: %+v, %v", opened, err)
	}
}
