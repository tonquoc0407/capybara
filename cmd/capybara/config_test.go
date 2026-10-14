package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadThemeDefaultsWhenConfigMissing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	th, err := loadTheme()
	if err != nil {
		t.Fatalf("loadTheme: %v", err)
	}
	if th.Name == "" {
		t.Error("expected a default theme")
	}
}

func TestLoadThemeReadsNamedTheme(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	confDir := filepath.Join(dir, "capybara")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(confDir, "config.toml"), []byte(`theme = "mono"`), 0o600); err != nil {
		t.Fatal(err)
	}
	th, err := loadTheme()
	if err != nil {
		t.Fatalf("loadTheme: %v", err)
	}
	if th.Name != "mono" {
		t.Errorf("theme = %q, want mono", th.Name)
	}
}

func TestLoadThemeRejectsUnknownName(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	confDir := filepath.Join(dir, "capybara")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(confDir, "config.toml"), []byte(`theme = "nope"`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadTheme(); err == nil {
		t.Fatal("expected an error for an unknown theme name")
	}
}
