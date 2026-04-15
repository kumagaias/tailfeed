package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config is the structure of ~/.config/tailfeed/mcp.json.
type Config struct {
	Command  string            `json:"command"`
	Args     []string          `json:"args,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	Language string            `json:"language,omitempty"` // e.g. "Japanese", "English". default: "Japanese"
}

// SummaryLanguage returns the language to use for summaries.
func (c Config) SummaryLanguage() string {
	if c.Language != "" {
		return c.Language
	}
	return "Japanese"
}

// Load reads ~/.config/tailfeed/mcp.json. Returns nil when not configured.
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
		return nil, fmt.Errorf("mcp.json: %w", err)
	}
	return &cfg, nil
}

// Save writes cfg to ~/.config/tailfeed/mcp.json. nil removes the file.
func Save(cfg *Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if cfg == nil {
		err := os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
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
	return filepath.Join(home, ".config", "tailfeed", "mcp.json"), nil
}
