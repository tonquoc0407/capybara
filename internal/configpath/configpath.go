// Package configpath resolves user configuration without migrating legacy files.
package configpath

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// File returns the path for a configuration file. XDG_CONFIG_HOME is an explicit
// override on every OS. Otherwise new files use os.UserConfigDir; a legacy file
// in ~/.config/capybara is read only when its native counterpart is absent.
func File(name string) string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "capybara", name)
	}
	home, homeErr := os.UserHomeDir()
	legacy := ""
	if homeErr == nil {
		legacy = filepath.Join(home, ".config", "capybara")
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		if legacy != "" {
			return filepath.Join(legacy, name)
		}
		dir = "."
	}
	return existingFile(name, filepath.Join(dir, "capybara"), legacy)
}

func existingFile(name, dir, legacy string) string {
	path := filepath.Join(dir, name)
	// An unreadable native file must report its error, not silently fall back.
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		return path
	}
	if legacy != "" && legacy != dir {
		old := filepath.Join(legacy, name)
		if _, err := os.Stat(old); !errors.Is(err, fs.ErrNotExist) {
			return old
		}
	}
	return path
}
