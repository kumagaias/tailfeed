package summary

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config stores user preferences for generated summaries.
type Config struct {
	Theme string `json:"theme,omitempty"`
}

// LoadConfig reads ~/.config/tailfeed/summary.json.
func LoadConfig() (*Config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("summary.json: %w", err)
	}
	cfg.Theme = strings.TrimSpace(cfg.Theme)
	return &cfg, nil
}

// SaveConfig writes ~/.config/tailfeed/summary.json.
func SaveConfig(cfg *Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = &Config{}
	}
	cfg.Theme = strings.TrimSpace(cfg.Theme)
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
	return filepath.Join(home, ".config", "tailfeed", "summary.json"), nil
}
