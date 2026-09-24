package ai

import (
	"context"
	"strings"
)

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

func summarizeWithChat(ctx context.Context, chat ChatClient, text string) (string, error) {
	messages := summaryMessages(ctx, text)
	if streaming, ok := chat.(StreamingChatClient); ok {
		var answer strings.Builder
		if err := streaming.StreamChat(ctx, messages, func(delta string) error {
			_, err := answer.WriteString(delta)
			return err
		}); err != nil {
			return "", err
		}
		return strings.TrimSpace(stripThinkTags(answer.String())), nil
	}
	answer, err := chat.Chat(ctx, messages)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(stripThinkTags(answer)), nil
}
