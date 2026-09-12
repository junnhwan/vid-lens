package config

import (
	"strings"
	"testing"
	"vid-lens/internal/model"
)

func TestAgentBudgetOptionsValidationAndResolution(t *testing.T) {
	cfg := DefaultAgentBudgetConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	got, err := cfg.Resolve(nil)
	if err != nil || got.Source != "server_default" || got.Values.MaxInputTokens != 65536 {
		t.Fatalf("resolve: %+v %v", got, err)
	}
	override := cfg.Defaults
	override.MaxToolCalls = 3
	override.MaxInputTokens = 8193
	override.MaxVisualFrames = nil
	if err := cfg.ValidateOverride(&override); err != nil {
		t.Fatalf("arbitrary integers rejected: %v", err)
	}
	got, err = cfg.Resolve(&override)
	if err != nil || *got.Values.MaxVisualFrames != 8 || got.Source != "profile" {
		t.Fatalf("override: %+v %v", got, err)
	}
	override.MaxToolCalls = 33
	if err := cfg.ValidateOverride(&override); err == nil || !strings.Contains(err.Error(), "max_tool_calls") {
		t.Fatalf("wanted field validation: %v", err)
	}
	got, err = cfg.Resolve(&override)
	if err != nil || got.Values.MaxToolCalls != 32 || len(got.Adjustments) != 1 || override.MaxToolCalls != 33 {
		t.Fatalf("clamp must be disclosed without mutating stored override: %+v %v", got, err)
	}
	override.Version = 2
	if _, err = cfg.Resolve(&override); err == nil {
		t.Fatal("unknown stored schema accepted")
	}
	override = model.AgentBudgetOverride{Version: 1}
	if _, err = cfg.Resolve(&override); err == nil {
		t.Fatal("corrupt stored values defaulted")
	}
	cfg = DefaultAgentBudgetConfig()
	cfg.FinalAnswerReserve.DurationSeconds = cfg.Defaults.MaxDurationSeconds
	if err := cfg.Validate(); err == nil {
		t.Fatal("reserve exceeding default total accepted")
	}
}

func TestShortProfileBudgetDisclosesReducedFinalTimeReserve(t *testing.T) {
	cfg := DefaultAgentBudgetConfig()
	override := cfg.Defaults
	override.MaxDurationSeconds = 90
	got, err := cfg.Resolve(&override)
	if err != nil || got.FinalAnswerReserve.DurationSeconds != 45 || len(got.Adjustments) != 1 || override.MaxDurationSeconds != 90 {
		t.Fatalf("resolution=%+v err=%v", got, err)
	}
	defaults, err := cfg.Resolve(nil)
	if err != nil || defaults.FinalAnswerReserve.DurationSeconds != 240 || defaults.Values.MaxDurationSeconds != 480 {
		t.Fatalf("default resolution=%+v err=%v", defaults, err)
	}
}
