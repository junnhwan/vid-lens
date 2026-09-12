package config

import (
	"fmt"
	"vid-lens/internal/model"
)

type AgentBudgetRange struct {
	Min  int    `json:"min" yaml:"min"`
	Max  int    `json:"max" yaml:"max"`
	Unit string `json:"unit" yaml:"unit"`
}
type AgentBudgetLimits struct {
	MaxToolCalls       AgentBudgetRange `json:"max_tool_calls" yaml:"max_tool_calls"`
	MaxDurationSeconds AgentBudgetRange `json:"max_duration_seconds" yaml:"max_duration_seconds"`
	MaxInputTokens     AgentBudgetRange `json:"max_input_tokens" yaml:"max_input_tokens"`
	MaxOutputTokens    AgentBudgetRange `json:"max_output_tokens" yaml:"max_output_tokens"`
	MaxVisualFrames    AgentBudgetRange `json:"max_visual_frames" yaml:"max_visual_frames"`
}
type AgentFinalAnswerReserve struct {
	ToolCalls       int `json:"tool_calls" yaml:"tool_calls"`
	LLMCalls        int `json:"llm_calls" yaml:"llm_calls"`
	InputTokens     int `json:"input_tokens" yaml:"input_tokens"`
	OutputTokens    int `json:"output_tokens" yaml:"output_tokens"`
	DurationSeconds int `json:"duration_seconds" yaml:"duration_seconds"`
}
type AgentBudgetConfig struct {
	Defaults           model.AgentBudgetOverride `json:"defaults" yaml:"defaults"`
	Limits             AgentBudgetLimits         `json:"limits" yaml:"limits"`
	FinalAnswerReserve AgentFinalAnswerReserve   `json:"final_answer_reserve" yaml:"final_answer_reserve"`
}
type ResolvedAgentBudget struct {
	Values             model.AgentBudgetOverride `json:"values"`
	Source             string                    `json:"source"`
	Adjustments        []string                  `json:"adjustments,omitempty"`
	PolicyVersion      int                       `json:"policy_version"`
	FinalAnswerReserve AgentFinalAnswerReserve   `json:"final_answer_reserve"`
}

func DefaultAgentBudgetConfig() AgentBudgetConfig {
	frames := 8
	return AgentBudgetConfig{
		Defaults:           model.AgentBudgetOverride{Version: 1, MaxToolCalls: 8, MaxDurationSeconds: 480, MaxInputTokens: 65536, MaxOutputTokens: 24576, MaxVisualFrames: &frames},
		Limits:             AgentBudgetLimits{AgentBudgetRange{2, 32, "calls"}, AgentBudgetRange{90, 900, "seconds"}, AgentBudgetRange{8192, 262144, "tokens"}, AgentBudgetRange{2048, 32768, "tokens"}, AgentBudgetRange{1, 32, "frames"}},
		FinalAnswerReserve: AgentFinalAnswerReserve{1, 1, 4096, 2048, 240},
	}
}
func (c AgentBudgetConfig) fields(b *model.AgentBudgetOverride) []struct {
	name  string
	value *int
	limit AgentBudgetRange
} {
	fields := []struct {
		name  string
		value *int
		limit AgentBudgetRange
	}{
		{"max_tool_calls", &b.MaxToolCalls, c.Limits.MaxToolCalls}, {"max_duration_seconds", &b.MaxDurationSeconds, c.Limits.MaxDurationSeconds}, {"max_input_tokens", &b.MaxInputTokens, c.Limits.MaxInputTokens}, {"max_output_tokens", &b.MaxOutputTokens, c.Limits.MaxOutputTokens},
	}
	if b.MaxVisualFrames != nil {
		fields = append(fields, struct {
			name  string
			value *int
			limit AgentBudgetRange
		}{"max_visual_frames", b.MaxVisualFrames, c.Limits.MaxVisualFrames})
	}
	return fields
}
func (c AgentBudgetConfig) ValidateOverride(b *model.AgentBudgetOverride) error {
	if b == nil {
		return nil
	}
	if b.Version != 1 {
		return fmt.Errorf("agent_budget.version: only version 1 is supported")
	}
	for _, f := range c.fields(b) {
		if *f.value < f.limit.Min || *f.value > f.limit.Max {
			return fmt.Errorf("agent_budget.%s: must be between %d and %d", f.name, f.limit.Min, f.limit.Max)
		}
	}
	return nil
}
func (c AgentBudgetConfig) Validate() error {
	if c.Defaults.MaxVisualFrames == nil {
		return fmt.Errorf("agent_budget.defaults.max_visual_frames is required")
	}
	for _, f := range c.fields(&c.Defaults) {
		if f.limit.Min <= 0 || f.limit.Max < f.limit.Min || f.limit.Max > 1000000000 {
			return fmt.Errorf("agent_budget.limits.%s: invalid range", f.name)
		}
	}
	if err := c.ValidateOverride(&c.Defaults); err != nil {
		return err
	}
	r := c.FinalAnswerReserve
	if r.ToolCalls < 1 || r.LLMCalls < 1 || r.InputTokens < 1 || r.OutputTokens < 1 || r.DurationSeconds < 1 || r.ToolCalls >= c.Limits.MaxToolCalls.Min || r.InputTokens >= c.Limits.MaxInputTokens.Min || r.OutputTokens > c.Limits.MaxOutputTokens.Min || r.DurationSeconds >= c.Defaults.MaxDurationSeconds {
		return fmt.Errorf("agent_budget.final_answer_reserve: must fit minimum total budgets")
	}
	return nil
}
func (c AgentBudgetConfig) Resolve(override *model.AgentBudgetOverride) (ResolvedAgentBudget, error) {
	if err := c.Validate(); err != nil {
		return ResolvedAgentBudget{}, err
	}
	result := ResolvedAgentBudget{Values: c.Defaults, Source: "server_default", PolicyVersion: 1, FinalAnswerReserve: c.FinalAnswerReserve}
	frames := *c.Defaults.MaxVisualFrames
	result.Values.MaxVisualFrames = &frames
	if override != nil {
		if override.Version != 1 {
			return ResolvedAgentBudget{}, fmt.Errorf("agent_budget.version: unsupported stored version")
		}
		result.Values = *override
		result.Source = "profile"
		if override.MaxVisualFrames != nil {
			frames = *override.MaxVisualFrames
		}
		result.Values.MaxVisualFrames = &frames
		for _, f := range c.fields(&result.Values) {
			if *f.value <= 0 {
				return ResolvedAgentBudget{}, fmt.Errorf("agent_budget.%s: invalid stored value", f.name)
			}
			if *f.value > f.limit.Max {
				result.Adjustments = append(result.Adjustments, fmt.Sprintf("%s: %d → %d (server limit)", f.name, *f.value, f.limit.Max))
				*f.value = f.limit.Max
			}
			if *f.value < f.limit.Min {
				result.Adjustments = append(result.Adjustments, fmt.Sprintf("%s: %d → %d (server minimum)", f.name, *f.value, f.limit.Min))
				*f.value = f.limit.Min
			}
		}
	}
	// Short custom runs keep exploration room. The effective reserve, including
	// this reduction, is returned to clients and frozen with the run.
	if limit := result.Values.MaxDurationSeconds / 2; result.FinalAnswerReserve.DurationSeconds > limit {
		result.FinalAnswerReserve.DurationSeconds = limit
		result.Adjustments = append(result.Adjustments, fmt.Sprintf("final_answer_reserve.duration_seconds: %d → %d (half of total duration)", c.FinalAnswerReserve.DurationSeconds, limit))
	}
	return result, nil
}
