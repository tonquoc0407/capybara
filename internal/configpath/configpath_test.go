package configpath

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileHonorsXDGOnEveryOS(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	want := filepath.Join(dir, "capybara", "config.toml")
	if got := File("config.toml"); got != want {
		t.Fatalf("File = %q, want %q", got, want)
	}
}

func TestFileUsesNativeRootForNewFiles(t *testing.T) {
	isolateConfigRoots(t)
	t.Setenv("XDG_CONFIG_HOME", "")
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Skip(err)
	}
	want := filepath.Join(dir, "capybara", "missing-portability-test.toml")
	if got := File("missing-portability-test.toml"); got != want {
		t.Fatalf("File = %q, want %q", got, want)
	}
}

func TestFileReadsLegacyUnlessXDGIsExplicit(t *testing.T) {
	home := isolateConfigRoots(t)
	t.Setenv("XDG_CONFIG_HOME", "")
	legacyDir := filepath.Join(home, ".config", "capybara")
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(legacyDir, "mapping.toml")
	if err := os.WriteFile(legacy, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := File("mapping.toml"); got != legacy {
		t.Fatalf("legacy File = %q, want %q", got, legacy)
	}
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	want := filepath.Join(xdg, "capybara", "mapping.toml")
	if got := File("mapping.toml"); got != want {
		t.Fatalf("explicit XDG File = %q, want %q", got, want)
	}
}

func isolateConfigRoots(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", t.TempDir())
	return home
}

func TestExistingFilePreservesLegacyPerFile(t *testing.T) {
	native, legacy := t.TempDir(), t.TempDir()
	old := filepath.Join(legacy, "pricing.json")
	if err := os.WriteFile(old, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := existingFile("pricing.json", native, legacy); got != old {
		t.Fatalf("legacy File = %q, want %q", got, old)
	}
	want := filepath.Join(native, "config.toml")
	if got := existingFile("config.toml", native, legacy); got != want {
		t.Fatalf("new File = %q, want %q", got, want)
	}
	current := filepath.Join(native, "pricing.json")
	if err := os.WriteFile(current, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := existingFile("pricing.json", native, legacy); got != current {
		t.Fatalf("native File = %q, want %q", got, current)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatalf("legacy file must remain untouched: %v", err)
	}
}

func TestExistingFileDoesNotHideInvalidNativePath(t *testing.T) {
	native, legacy := t.TempDir(), t.TempDir()
	// A directory where a config file should be must not be ignored.
	path := filepath.Join(native, "mapping.toml")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "mapping.toml"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := existingFile("mapping.toml", native, legacy); got != path {
		t.Fatalf("File = %q, want invalid native path %q", got, path)
	}
}
