package config

import (
	"gopkg.in/yaml.v3"
	"os"
	"strings"
	"testing"
	"vid-lens/internal/model"
)

func TestAgentBudgetDefaultsAndPackagedConfigAgree(t *testing.T) {
	want := DefaultAgentBudgetConfig()
	if want.Defaults.MaxToolCalls != 32 || want.Defaults.MaxDurationSeconds != 1200 || want.Defaults.MaxInputTokens != 262144 || want.Defaults.MaxOutputTokens != 65536 {
		t.Fatalf("defaults too small for multi-segment work: %+v", want.Defaults)
	}
	data, err := os.ReadFile("../../config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"TOOL_CALLS", "DURATION_SECONDS", "INPUT_TOKENS", "OUTPUT_TOKENS", "VISUAL_FRAMES", "MAX_TOOL_CALLS", "MAX_DURATION_SECONDS", "MAX_INPUT_TOKENS", "MAX_OUTPUT_TOKENS", "MAX_VISUAL_FRAMES"} {
		t.Setenv("AGENT_BUDGET_"+name, "")
	}
	var packaged struct {
		AgentBudget AgentBudgetConfig `yaml:"agent_budget"`
	}
	if err = yaml.Unmarshal(expandConfigEnvironment(data), &packaged); err != nil {
		t.Fatal(err)
	}
	if err = packaged.AgentBudget.Validate(); err != nil {
		t.Fatal(err)
	}
	got, _ := yaml.Marshal(packaged.AgentBudget)
	expected, _ := yaml.Marshal(want)
	if string(got) != string(expected) {
		t.Fatalf("packaged budget diverged:\n%s\nwant:\n%s", got, expected)
	}
	// Existing explicitly chosen budgets continue to fit the widened limits.
	old := model.AgentBudgetOverride{Version: 1, MaxToolCalls: 8, MaxDurationSeconds: 480, MaxInputTokens: 65536, MaxOutputTokens: 24576}
	resolved, err := want.Resolve(&old)
	if err != nil || resolved.Values.MaxToolCalls != 8 || resolved.Values.MaxInputTokens != 65536 || len(resolved.Adjustments) != 0 {
		t.Fatalf("custom budget changed: %+v %v", resolved, err)
	}
}

func TestAgentBudgetOptionsValidationAndResolution(t *testing.T) {
	cfg := DefaultAgentBudgetConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	got, err := cfg.Resolve(nil)
	if err != nil || got.Source != "server_default" || got.Values.MaxInputTokens != 262144 {
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
	override.MaxToolCalls = cfg.Limits.MaxToolCalls.Max + 1
	if err := cfg.ValidateOverride(&override); err == nil || !strings.Contains(err.Error(), "max_tool_calls") {
		t.Fatalf("wanted field validation: %v", err)
	}
	got, err = cfg.Resolve(&override)
	if err != nil || got.Values.MaxToolCalls != cfg.Limits.MaxToolCalls.Max || len(got.Adjustments) != 1 || override.MaxToolCalls != cfg.Limits.MaxToolCalls.Max+1 {
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
	if err != nil || defaults.FinalAnswerReserve.DurationSeconds != 240 || defaults.Values.MaxDurationSeconds != 1200 {
		t.Fatalf("default resolution=%+v err=%v", defaults, err)
	}
}
