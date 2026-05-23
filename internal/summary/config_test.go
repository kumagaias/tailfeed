package summary

import (
	"strings"
	"testing"
)

func TestSummaryConfigSavesTheme(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if err := SaveConfig(&Config{Theme: "  AI infra  ", Language: "  English  "}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Theme != "AI infra" {
		t.Fatalf("Theme = %q, want %q", cfg.Theme, "AI infra")
	}
	if cfg.Language != "English" {
		t.Fatalf("Language = %q, want %q", cfg.Language, "English")
	}
}

func TestSummaryConfigDefaultsLanguageToJapanese(t *testing.T) {
	var cfg Config
	if got := cfg.SummaryLanguage(); got != "Japanese" {
		t.Fatalf("SummaryLanguage = %q, want Japanese", got)
	}
}

func TestThemeWithLanguageInstruction(t *testing.T) {
	got := ThemeWithLanguageInstruction("AI infra", "Japanese")
	for _, want := range []string{
		"Write all generated summary content",
		"in Japanese",
		"Keep original article titles exactly as provided",
		"User theme: AI infra",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in theme instruction, got:\n%s", want, got)
		}
	}
}
