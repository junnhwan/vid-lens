package ai

import "context"

type ChatUsage struct {
	PromptTokens     int64
	CompletionTokens int64
}
type chatBudgetKey struct{}
type chatCallBudget struct {
	output int64
	report func(ChatUsage)
}

// WithChatBudget is request-local; ordinary chat and other AI tasks are unchanged.
func WithChatBudget(ctx context.Context, maxOutput int64, report func(ChatUsage)) context.Context {
	return context.WithValue(ctx, chatBudgetKey{}, chatCallBudget{maxOutput, report})
}

func applyChatBudget(ctx context.Context, body map[string]interface{}) {
	if budget, ok := ctx.Value(chatBudgetKey{}).(chatCallBudget); ok && budget.output > 0 {
		body["max_tokens"] = budget.output
	}
}

func reportChatUsage(ctx context.Context, prompt, completion int64) {
	if budget, ok := ctx.Value(chatBudgetKey{}).(chatCallBudget); ok && budget.report != nil {
		budget.report(ChatUsage{prompt, completion})
	}
}
