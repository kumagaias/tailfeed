package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config is stored in ~/.config/tailfeed/api.json.
type Config struct {
	UserKey string `json:"user_key"`
	Tier    string `json:"tier"`
}

// Load reads ~/.config/tailfeed/api.json. Returns nil when not configured.
func Load() (*Config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("api.json: %w", err)
	}
	return &cfg, nil
}

// Save writes cfg to ~/.config/tailfeed/api.json.
func Save(cfg *Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "tailfeed", "api.json"), nil
}
