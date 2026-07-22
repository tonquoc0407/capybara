package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/tonquoc0407/capybara/internal/theme"
)

type config struct {
	Theme string `toml:"theme"`
}

func loadTheme() (theme.Theme, error) {
	path := filepath.Join(configDir(), "capybara", "config.toml")
	var cfg config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return theme.All()[0], nil
		}
		return theme.Theme{}, fmt.Errorf("read %s: %w", path, err)
	}
	if cfg.Theme == "" {
		return theme.All()[0], nil
	}
	th, ok := theme.ByName(cfg.Theme)
	if !ok {
		names := make([]string, 0, len(theme.All()))
		for _, t := range theme.All() {
			names = append(names, t.Name)
		}
		return theme.Theme{}, fmt.Errorf("unknown theme %q in %s (have: %v)", cfg.Theme, path, names)
	}
	return th, nil
}

func configDir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return x
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".config")
}
