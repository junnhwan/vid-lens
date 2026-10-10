// Package processing contains the frozen import contract, shared by HTTP and
// durable workers. It contains no credentials or worker status.
package processing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
	"vid-lens/internal/ai"
)

const Recipe = "summary-generation-v2"

type Options struct {
	AutoSummary          bool   `json:"auto_summary"`
	TextSourcePolicy     string `json:"text_source_policy"`
	PreferredLanguage    string `json:"preferred_language"`
	SummaryVisualEnabled bool   `json:"summary_visual_enabled"`
	OutputMode           string `json:"output_mode"`
	MindmapEnabled       bool   `json:"mindmap_enabled"`
	SummaryInstruction   string `json:"summary_instruction"`
	AutoTagsEnabled      bool   `json:"auto_tags_enabled"`
	ProfileID            int64  `json:"profile_id,omitempty"`
}

// Intent freezes resolved settings at first acceptance. A changed profile must
// fail fingerprint validation rather than switch to the new default silently.
type Intent struct {
	ID                 string  `json:"id"`
	Version            int     `json:"version"`
	Options            Options `json:"options"`
	GenerationID       string  `json:"generation_id"`
	ProfileID          int64   `json:"profile_id"`
	ProfileFingerprint string  `json:"profile_fingerprint"`
	SummaryPreference  string  `json:"summary_preference"`
	PolicyJSON         string  `json:"policy_json"`
	BudgetJSON         string  `json:"budget_json"`
	RecipeVersion      string  `json:"recipe_version"`
}

type GenerationSnapshot struct {
	Operation                 string               `json:"operation,omitempty"`
	VisualRetry               *VisualRetrySnapshot `json:"visual_retry,omitempty"`
	Intent                    Intent               `json:"intent"`
	SourceID                  string               `json:"source_id"`
	SourceDigest              string               `json:"source_digest"`
	ExpectedGeneratedVersion  int64                `json:"expected_generated_version"`
	ExpectedGeneratedHash     string               `json:"expected_generated_hash"`
	ExpectedGeneratedHashKind string               `json:"expected_generated_hash_kind"`
}

func Normalize(options Options, local bool) (Options, error) {
	options.TextSourcePolicy = strings.TrimSpace(options.TextSourcePolicy)
	if options.TextSourcePolicy == "" {
		options.TextSourcePolicy = "prefer_platform"
	}
	if local {
		options.TextSourcePolicy = "force_asr"
	}
	if options.TextSourcePolicy != "prefer_platform" && options.TextSourcePolicy != "force_asr" {
		return Options{}, fmt.Errorf("unsupported text_source_policy")
	}
	options.OutputMode = strings.TrimSpace(options.OutputMode)
	if options.OutputMode == "" {
		options.OutputMode = "auto"
	}
	if options.OutputMode != "auto" && options.OutputMode != "text" && options.OutputMode != "image_text" && options.OutputMode != "keyframes" {
		return Options{}, fmt.Errorf("unsupported output_mode")
	}
	if !options.SummaryVisualEnabled && (options.OutputMode == "image_text" || options.OutputMode == "keyframes") {
		return Options{}, fmt.Errorf("visual output requires summary_visual_enabled")
	}
	options.SummaryInstruction = strings.TrimSpace(options.SummaryInstruction)
	options.PreferredLanguage = strings.TrimSpace(options.PreferredLanguage)
	if !utf8.ValidString(options.SummaryInstruction) || utf8.RuneCountInString(options.SummaryInstruction) > 2000 || len(options.PreferredLanguage) > 40 || options.ProfileID < 0 {
		return Options{}, fmt.Errorf("invalid processing options")
	}
	return options, nil
}

func Decode(raw string) (Intent, error) {
	var intent Intent
	if err := json.Unmarshal([]byte(raw), &intent); err != nil {
		return intent, fmt.Errorf("invalid frozen processing intent")
	}
	if intent.Version != 1 || intent.ID == "" || intent.GenerationID == "" || intent.RecipeVersion != Recipe {
		return intent, fmt.Errorf("invalid frozen processing identity")
	}
	normalized, err := Normalize(intent.Options, false)
	if err != nil || normalized != intent.Options {
		return intent, fmt.Errorf("invalid frozen processing options")
	}
	return intent, nil
}

func Fingerprint(value any) string {
	raw, _ := json.Marshal(value)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

// FingerprintProfile includes provider and model settings for every operation
// in the import. Credentials stay in their encrypted profile storage.
func FingerprintProfile(p ai.Profile) string {
	return Fingerprint([]any{p.ID, p.LLMProvider, p.LLMBaseURL, p.LLMModel, p.LLMContextTokens, p.ASRProvider, p.ASRBaseURL, p.ASRModel, p.VisionProvider, p.VisionBaseURL, p.VisionModel, p.EmbeddingProvider, p.EmbeddingEndpoint, p.EmbeddingModel, p.EmbeddingDim})
}
