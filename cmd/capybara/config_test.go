package main

import (
	"context"
	"io"
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
	if th.Name != "pupu" {
		t.Errorf("default theme = %q, want pupu", th.Name)
	}
}

func TestDatabaseEnvironmentDefaultAndFlagOverride(t *testing.T) {
	dir := t.TempDir()
	envDB := filepath.Join(dir, "environment.db")
	flagDB := filepath.Join(dir, "flag.db")
	t.Setenv("CAPYBARA_DB", envDB)
	if err := run(context.Background(), []string{"runs"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(envDB); err != nil {
		t.Fatalf("environment database: %v", err)
	}
	if err := run(context.Background(), []string{"-db", flagDB, "runs"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(flagDB); err != nil {
		t.Fatalf("flag database: %v", err)
	}
}

func TestInvalidNoClaudeEnvironmentIsReported(t *testing.T) {
	t.Setenv("CAPYBARA_NO_CLAUDE", "yes")
	if err := run(context.Background(), []string{"-version"}, io.Discard); err == nil {
		t.Fatal("expected invalid CAPYBARA_NO_CLAUDE to fail")
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

func TestConfigDirUsesXDGWhenSet(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg/config")
	if got := configDir(); got != "/xdg/config" {
		t.Errorf("configDir = %q, want /xdg/config", got)
	}
}

func TestConfigDirFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir available")
	}
	if got := configDir(); got != filepath.Join(home, ".config") {
		t.Errorf("configDir = %q, want %s", got, filepath.Join(home, ".config"))
	}
}
