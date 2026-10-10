package service

import (
	"context"
	"sync"

	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

// One authorized inspection can invoke at most one VLM. Unknown usage is
// charged conservatively at the output allowance, never as actual zero.
// The image prompt estimate is explicitly estimated, not provider billing.
const summaryVisionPromptEstimate int64 = 4096

type summaryGenerationBudgetVision struct {
	client ai.VisionClient
	cap    int64
	mu     sync.Mutex
	called bool
	usage  VideoAgentLoopPlannerCallUsage
}

func (c *summaryGenerationBudgetVision) CaptionImage(ctx context.Context, path, prompt string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.called || c.cap < 128 {
		return "", artifact.Err("visual_budget_exhausted", 422)
	}
	c.called = true
	var measured *ai.ChatUsage
	ctx = ai.WithStructuredJSON(ai.WithChatBudget(ctx, c.cap, func(u ai.ChatUsage) { measured = &u }))
	text, err := c.client.CaptionImage(ctx, path, prompt)
	c.usage = VideoAgentLoopPlannerCallUsage{PromptTokens: summaryVisionPromptEstimate + studyPromptTokens([]ai.ChatMessage{{Role: "user", Content: prompt}}), CompletionTokens: c.cap, UsageSource: model.AgentCallUsageEstimated, TokenEstimated: true}
	if measured != nil {
		c.usage.PromptTokens = measured.PromptTokens
		c.usage.CompletionTokens = measured.CompletionTokens
		c.usage.UsageSource = model.AgentCallUsageActual
		c.usage.TokenEstimated = false
	}
	return text, err
}
func (c *summaryGenerationBudgetVision) measuredUsage(reportedCalls int) VideoAgentLoopPlannerCallUsage {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.called {
		return c.usage
	}
	if reportedCalls > 0 {
		return VideoAgentLoopPlannerCallUsage{PromptTokens: summaryVisionPromptEstimate, CompletionTokens: c.cap, UsageSource: model.AgentCallUsageEstimated, TokenEstimated: true}
	}
	return VideoAgentLoopPlannerCallUsage{UsageSource: model.AgentCallUsageUnknown}
}
