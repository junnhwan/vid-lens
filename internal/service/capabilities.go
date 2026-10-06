package service

import (
	"context"
	"os/exec"
	"time"
	"vid-lens/internal/ai"
)

type CapabilityState struct {
	Key               string     `json:"key"`
	Activation        string     `json:"activation"`
	Configured        bool       `json:"configured"`
	DeploymentEnabled bool       `json:"deployment_enabled"`
	UserEnabled       *bool      `json:"user_enabled"`
	Available         bool       `json:"available"`
	EffectiveEnabled  bool       `json:"effective_enabled"`
	Health            string     `json:"health"`
	ReasonCode        string     `json:"reason_code,omitempty"`
	Model             string     `json:"model,omitempty"`
	CheckedAt         *time.Time `json:"checked_at,omitempty"`
	Version           string     `json:"version,omitempty"`
	InstallURL        string     `json:"install_url,omitempty"`
}

// This is model/tool admission only; it is never a resource authorization token.
type ActionState struct {
	Action               string   `json:"action"`
	Allowed              bool     `json:"allowed"`
	RequiredCapabilities []string `json:"required_capabilities"`
	ReasonCode           string   `json:"reason_code,omitempty"`
}

func modelCapabilityStates(p ai.Profile) []CapabilityState {
	states := []CapabilityState{}
	for _, item := range []struct{ key, model string }{{"llm", p.LLMModel}, {"asr", p.ASRModel}, {"embedding", p.EmbeddingModel}, {"vision", p.VisionModel}} {
		configured := ai.ModelConfigured(p, item.key)
		reason := ""
		if !configured {
			reason = "missing_configuration"
		}
		states = append(states, CapabilityState{Key: item.key, Model: item.model, Activation: "mandatory", Configured: configured, DeploymentEnabled: true, Available: configured, EffectiveEnabled: configured, Health: "unchecked", ReasonCode: reason})
	}
	return states
}

func (s *UserService) projectCapabilities(ctx context.Context, view *OptionalCapabilitiesView) {
	if view == nil {
		return
	}
	if len(view.Capabilities) == 0 {
		view.Capabilities = modelCapabilityStates(ai.Profile{})
		for i := range view.Capabilities {
			view.Capabilities[i].ReasonCode = view.RerankReason
			if view.RerankReason == "ai_profile_required" {
				view.Capabilities[i].ReasonCode = "missing_configuration"
			}
		}
	}
	// Rebuild derived local states after PATCH as well as GET.
	view.Capabilities = view.Capabilities[:4]
	cfg := s.optionalCapabilities
	ocrConfigured, ocrAvailable := false, false
	if cfg != nil && cfg.tools.OCRPath != "" {
		ocrConfigured = true
		_, err := exec.LookPath(cfg.tools.OCRPath)
		ocrAvailable = err == nil
	}
	local := func(key string, configured, available bool, activation string) CapabilityState {
		reason := ""
		if !configured {
			reason = "deployment_disabled"
		} else if !available {
			reason = "dependency_missing"
		}
		return CapabilityState{Key: key, Activation: activation, Configured: configured, DeploymentEnabled: configured, Available: available, EffectiveEnabled: available, Health: "unchecked", ReasonCode: reason}
	}
	// Alignment configuration is not proof that weights or Python modules work.
	view.Capabilities = append(view.Capabilities, local("ocr", ocrConfigured, ocrAvailable, "video"), local("alignment", view.AlignmentConfigured, view.AlignmentConfigured, "manual"))
	if cfg != nil && cfg.inspector != nil {
		checks := cfg.inspector.Snapshot(ctx)
		for _, key := range []string{"ffmpeg", "ocr", "alignment", "url_import"} {
			check := checks[key]
			state := local(key, key == "ffmpeg" || key == "alignment" && view.AlignmentConfigured || key == "ocr" && cfg.tools.OCRPath != "" || key == "url_import" && cfg.tools.YtDlpPath != "", check.Available, "manual")
			state.Health, state.ReasonCode, state.CheckedAt, state.Version = check.Health, check.ReasonCode, &check.CheckedAt, check.Version
			state.InstallURL = "/docs/config#local-dependencies"
			replaced := false
			for n := range view.Capabilities {
				if view.Capabilities[n].Key == key {
					view.Capabilities[n] = state
					replaced = true
					break
				}
			}
			if !replaced {
				view.Capabilities = append(view.Capabilities, state)
			}
		}
	}
	rerank := local("rerank", view.RerankAvailable, view.RerankAvailable, "user")
	rerank.UserEnabled = &view.RerankEnabled
	rerank.EffectiveEnabled = view.RerankAvailable && view.RerankEnabled
	rerank.ReasonCode = view.RerankReason
	if view.RerankAvailable && !view.RerankEnabled {
		rerank.ReasonCode = "user_disabled"
	}
	view.Capabilities = append(view.Capabilities, rerank)
	states := map[string]CapabilityState{}
	for _, state := range view.Capabilities {
		states[state.Key] = state
	}
	view.Actions = map[string]ActionState{}
	for _, action := range []string{"upload", "transcribe", "summary", "study", "revise", "caption", "index", "chat", "agent", "align", "ocr"} {
		keys, _ := ai.ActionCapabilities(action)
		if action == "align" {
			keys = []string{"alignment"}
		}
		if action == "ocr" {
			keys = []string{"ocr"}
		}
		if cfg != nil && cfg.inspector != nil && (action == "transcribe" || action == "align" || action == "ocr" || action == "caption") {
			keys = append(keys, "ffmpeg")
		}
		entry := ActionState{Action: action, Allowed: true, RequiredCapabilities: keys}
		for _, key := range keys {
			state := states[key]
			if !state.EffectiveEnabled {
				entry.Allowed, entry.ReasonCode = false, state.ReasonCode
				break
			}
		}
		if cfg != nil && !cfg.ragEnabled && (action == "index" || action == "chat" || action == "agent") {
			entry.Allowed, entry.ReasonCode = false, "deployment_disabled"
		}
		view.Actions[action] = entry
	}
}
