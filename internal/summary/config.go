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
	Theme    string `json:"theme,omitempty"`
	Language string `json:"language,omitempty"`
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
	cfg.Language = strings.TrimSpace(cfg.Language)
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
	cfg.Language = strings.TrimSpace(cfg.Language)
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

// SummaryLanguage returns the configured default summary language.
func (c Config) SummaryLanguage() string {
	if c.Language != "" {
		return c.Language
	}
	return "Japanese"
}

// ThemeWithLanguageInstruction returns a theme string that also carries the
// user's summary language preference for API backends that primarily use theme.
func ThemeWithLanguageInstruction(theme, language string) string {
	language = strings.TrimSpace(language)
	if language == "" {
		language = "Japanese"
	}
	instruction := "Write all generated summary content, labels, and bullet text in " + language + ". Keep original article titles exactly as provided. Use simple list items; do not write labels like TL;DR, Summary, or Key Points before summary sentences."
	theme = strings.TrimSpace(theme)
	if theme == "" {
		return instruction
	}
	return instruction + " User theme: " + theme
}
