package service

import (
	"context"
	"encoding/json"
	"time"
	"vid-lens/internal/model"
)

type AgentBudgetNotice struct {
	Dimension     string `json:"dimension"`
	Used          int64  `json:"used"`
	EstimatedNext int64  `json:"estimated_next"`
	Reserve       int64  `json:"reserve"`
	Limit         int64  `json:"limit"`
	UsageSource   string `json:"usage_source"`
}

func budgetDimensionLabel(dimension string) string {
	switch dimension {
	case "tool_calls":
		return "工具调用"
	case "llm_calls":
		return "模型调用"
	case "input_tokens":
		return "输入 Token"
	case "output_tokens":
		return "输出 Token"
	case "duration_ms":
		return "执行时间（毫秒）"
	case "context_chars":
		return "上下文字符"
	default:
		return "执行额度"
	}
}

func (r *VideoAgentLoopRunner) explorationBudgetNotice(ctx context.Context, state VideoAgentLoopState, runtime VideoAgentToolRuntime) (*AgentBudgetNotice, error) {
	if r.execution == nil {
		return nil, nil
	}
	run, err := r.execution.journal.GetRun(ctx, r.execution.userID, r.execution.runID)
	if err != nil || run == nil {
		return nil, err
	}
	var budget frozenAgentBudget
	if err := json.Unmarshal([]byte(run.BudgetSnapshot), &budget); err != nil {
		return nil, err
	}
	// Historical executions retain their original contract.
	if budget.SchemaVersion < 1 {
		return nil, nil
	}
	messages, err := buildPlannerMessages(state, r.registry.Definitions())
	if err != nil {
		return nil, err
	}
	p := estimatedPlannerCallUsage(messages, "")
	finalMessages := buildCitedAnswerMessages(BuildCitedAnswerInput{ScopeTaskIDs: runtime.TaskIDs, Question: state.Goal, Recent: runtime.Recent, Citations: boundedFinalEvidence(state.Evidence), Intermediate: "根据已有证据说明已知与缺口"}, runtime.MemorySnapshot)
	f := estimatedPlannerCallUsage(finalMessages, "")
	reserveInput := max(budget.ReserveInputTokens, f.PromptTokens)
	checks := []AgentBudgetNotice{
		{Dimension: "tool_calls", Used: int64(run.ToolCallsUsed), EstimatedNext: 1, Reserve: 1, Limit: int64(run.MaxToolCalls)},
		{Dimension: "llm_calls", Used: int64(run.LLMCallsUsed), EstimatedNext: 1, Reserve: 1, Limit: int64(run.MaxLLMCalls)},
		{Dimension: "input_tokens", Used: run.PromptTokensUsed, EstimatedNext: p.PromptTokens, Reserve: reserveInput, Limit: run.MaxPromptTokens},
		{Dimension: "output_tokens", Used: run.CompletionTokensUsed, EstimatedNext: 1024, Reserve: budget.ReserveOutputTokens, Limit: run.MaxCompletionTokens},
		{Dimension: "duration_ms", Used: time.Since(run.CreatedAt).Milliseconds(), EstimatedNext: 30000, Reserve: budget.ReserveDurationMs, Limit: run.MaxDurationMs},
		{Dimension: "context_chars", Used: run.ContextCharsUsed, EstimatedNext: p.ContextChars, Reserve: f.ContextChars, Limit: run.MaxContextChars},
	}
	for _, check := range checks {
		if check.Limit > 0 && check.Used+check.EstimatedNext+check.Reserve > check.Limit {
			check.UsageSource = model.AgentCallUsageEstimated
			return &check, nil
		}
	}
	return nil, nil
}

func boundedFinalEvidence(evidence []RetrievedChunk) []RetrievedChunk {
	var result []RetrievedChunk
	for _, chunk := range balancedEvidence(evidence, 12) {
		if len(result) >= 12 {
			break
		}
		chunk.Content = trimRunes(chunk.Content, 600)
		chunk.AnchorContent = ""
		result = append(result, chunk)
	}
	return result
}
