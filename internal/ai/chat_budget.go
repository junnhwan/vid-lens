package ai

import (
	"context"
	"strings"
)

type ChatUsage struct {
	PromptTokens     int64
	CompletionTokens int64
	ReasoningTokens  int64
}
type structuredJSONKey struct{}

// WithStructuredJSON marks a bounded extraction/organization call. Compatible
// Qwen hybrid models use JSON mode with thinking disabled for these calls only.
func WithStructuredJSON(ctx context.Context) context.Context {
	return context.WithValue(ctx, structuredJSONKey{}, true)
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
	if enabled, _ := ctx.Value(structuredJSONKey{}).(bool); enabled {
		model, _ := body["model"].(string)
		model = strings.ToLower(model)
		if i := strings.LastIndexByte(model, '/'); i >= 0 {
			model = model[i+1:]
		}
		// Limit provider extensions to known hybrid families. Unrelated models
		// retain their existing request contract and strict server validation.
		if strings.HasPrefix(model, "qwen3.5-") || strings.HasPrefix(model, "qwen3.6-") || strings.HasPrefix(model, "qwen3.7-") {
			body["enable_thinking"] = false
			body["response_format"] = map[string]string{"type": "json_object"}
		}
	}
}

func reportChatUsage(ctx context.Context, prompt, completion, reasoning int64) {
	if budget, ok := ctx.Value(chatBudgetKey{}).(chatCallBudget); ok && budget.report != nil {
		budget.report(ChatUsage{PromptTokens: prompt, CompletionTokens: completion, ReasoningTokens: reasoning})
	}
}
