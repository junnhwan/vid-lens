package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Omission preserves existing installations. These switches control new
// admission and optional visual execution, never authorization or reading.
type SummaryExperienceConfig struct {
	V2GenerationEnabled     *bool `yaml:"v2_generation_enabled,omitempty"`
	VisualEnrichmentEnabled *bool `yaml:"visual_enrichment_enabled,omitempty"`
}

func (c SummaryExperienceConfig) GenerationEnabled() bool {
	return c.V2GenerationEnabled == nil || *c.V2GenerationEnabled
}

func (c SummaryExperienceConfig) VisualEnabled() bool {
	return c.VisualEnrichmentEnabled == nil || *c.VisualEnrichmentEnabled
}

func (c *SummaryExperienceConfig) applyEnvironment() error {
	for name, target := range map[string]**bool{
		"VIDLENS_SUMMARY_V2_GENERATION_ENABLED":     &c.V2GenerationEnabled,
		"VIDLENS_SUMMARY_VISUAL_ENRICHMENT_ENABLED": &c.VisualEnrichmentEnabled,
	} {
		if raw, exists := os.LookupEnv(name); exists {
			value, err := strconv.ParseBool(strings.TrimSpace(raw))
			if err != nil {
				return fmt.Errorf("%s 必须为布尔值", name)
			}
			*target = &value
		}
	}
	return nil
}
