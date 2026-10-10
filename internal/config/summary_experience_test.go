package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadSummaryPolicy(t *testing.T, content string) *Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestSummaryExperienceDefaultsAndExplicitYAML(t *testing.T) {
	cfg := loadSummaryPolicy(t, "{}\n")
	if !cfg.SummaryExperience.GenerationEnabled() || !cfg.SummaryExperience.VisualEnabled() {
		t.Fatal("omitted policy broke existing installations")
	}
	cfg = loadSummaryPolicy(t, "summary_experience:\n  v2_generation_enabled: false\n  visual_enrichment_enabled: false\n")
	if cfg.SummaryExperience.GenerationEnabled() || cfg.SummaryExperience.VisualEnabled() {
		t.Fatal("explicit deployment disable ignored")
	}
}

func TestSummaryExperienceEnvironmentWinsAndRejectsInvalid(t *testing.T) {
	t.Setenv("VIDLENS_SUMMARY_V2_GENERATION_ENABLED", "true")
	t.Setenv("VIDLENS_SUMMARY_VISUAL_ENRICHMENT_ENABLED", "false")
	cfg := loadSummaryPolicy(t, "summary_experience:\n  v2_generation_enabled: false\n  visual_enrichment_enabled: true\n")
	if !cfg.SummaryExperience.GenerationEnabled() || cfg.SummaryExperience.VisualEnabled() {
		t.Fatal("environment did not override the YAML")
	}
	t.Setenv("VIDLENS_SUMMARY_V2_GENERATION_ENABLED", "maybe")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "VIDLENS_SUMMARY_V2_GENERATION_ENABLED") {
		t.Fatal("invalid deployment switch was silently ignored")
	}
}
