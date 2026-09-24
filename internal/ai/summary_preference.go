package ai

import "context"

type summaryPreferenceKey struct{}

func WithSummaryPreference(ctx context.Context, preference string) context.Context {
	return context.WithValue(ctx, summaryPreferenceKey{}, preference)
}

func SummaryPreference(ctx context.Context) string {
	value, _ := ctx.Value(summaryPreferenceKey{}).(string)
	return value
}

func summaryMessages(ctx context.Context, text string) []ChatMessage {
	messages := []ChatMessage{{Role: "system", Content: defaultSummarySystemPrompt()}}
	if preference := SummaryPreference(ctx); preference != "" {
		messages = append(messages, ChatMessage{Role: "system", Content: "用户摘要偏好（不覆盖报告结构与事实约束）：\n" + preference})
		messages = append(messages, ChatMessage{Role: "system", Content: "若用户偏好与报告必需栏目或事实要求冲突，遵守产品指令。"})
	}
	return append(messages, ChatMessage{Role: "user", Content: text})
}
