package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// AgentBudgetOverride is a complete versioned user override; visual frames may follow defaults.
type AgentBudgetOverride struct {
	Version            int  `json:"version" yaml:"version"`
	MaxToolCalls       int  `json:"max_tool_calls" yaml:"max_tool_calls"`
	MaxDurationSeconds int  `json:"max_duration_seconds" yaml:"max_duration_seconds"`
	MaxInputTokens     int  `json:"max_input_tokens" yaml:"max_input_tokens"`
	MaxOutputTokens    int  `json:"max_output_tokens" yaml:"max_output_tokens"`
	MaxVisualFrames    *int `json:"max_visual_frames,omitempty" yaml:"max_visual_frames"`
}

func DecodeAgentBudget(data []byte) (*AgentBudgetOverride, error) {
	var budget AgentBudgetOverride
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&budget); err != nil {
		return nil, fmt.Errorf("agent_budget: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("agent_budget: expected one JSON object")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, fmt.Errorf("agent_budget: %w", err)
	}
	if raw, ok := fields["max_visual_frames"]; ok && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, fmt.Errorf("agent_budget.max_visual_frames: expected an integer or omit field")
	}
	if budget.Version != 1 {
		return nil, fmt.Errorf("agent_budget.version: only version 1 is supported")
	}
	if budget.MaxToolCalls <= 0 || budget.MaxDurationSeconds <= 0 || budget.MaxInputTokens <= 0 || budget.MaxOutputTokens <= 0 {
		return nil, fmt.Errorf("agent_budget: all general fields must be positive integers")
	}
	if budget.MaxVisualFrames != nil && *budget.MaxVisualFrames <= 0 {
		return nil, fmt.Errorf("agent_budget.max_visual_frames: must be positive")
	}
	return &budget, nil
}

// AgentBudgetField distinguishes omitted updates, explicit default reset, and replacement.
type AgentBudgetField struct {
	Present bool
	Value   *AgentBudgetOverride
}

func (f *AgentBudgetField) UnmarshalJSON(data []byte) error {
	f.Present = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		f.Value = nil
		return nil
	}
	value, err := DecodeAgentBudget(data)
	if err != nil {
		return err
	}
	f.Value = value
	return nil
}
func (f AgentBudgetField) MarshalJSON() ([]byte, error) { return json.Marshal(f.Value) }
