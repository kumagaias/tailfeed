package summary

import "testing"

func TestSummaryConfigSavesTheme(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if err := SaveConfig(&Config{Theme: "  AI infra  "}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Theme != "AI infra" {
		t.Fatalf("Theme = %q, want %q", cfg.Theme, "AI infra")
	}
}
