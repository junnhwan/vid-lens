package ai

import (
	"context"
	"fmt"
	"strings"
)

type summaryPreferenceKey struct{}
type summaryIntermediateLimitKey struct{}

// WithSummaryIntermediateLimit asks for compact notes that fit the next merge,
// while final summaries retain the full report structure.
func WithSummaryIntermediateLimit(ctx context.Context, maxChars int) context.Context {
	return context.WithValue(ctx, summaryIntermediateLimitKey{}, maxChars)
}

func WithSummaryPreference(ctx context.Context, preference string) context.Context {
	return context.WithValue(ctx, summaryPreferenceKey{}, preference)
}

func SummaryPreference(ctx context.Context) string {
	value, _ := ctx.Value(summaryPreferenceKey{}).(string)
	return value
}

func summaryMessages(ctx context.Context, text string) []ChatMessage {
	prompt := defaultSummarySystemPrompt()
	if maxChars, _ := ctx.Value(summaryIntermediateLimitKey{}).(int); maxChars > 0 {
		prompt = fmt.Sprintf("你正在为视频全片摘要准备中间事实笔记。只根据输入提炼各段独有的重要事实、关键术语、因果关系和时间线，删除重复表达。全文最多 %d 个字符（含标点、空格及格式符号）。使用紧凑短句，不写开场白，不重复报告的四个栏目；输入中要求完整报告的文字在这一步不适用，完整报告由最终汇总生成。必须阅读全部输入，不得只概括开头或省略末尾的独有要点。保留比较对象及各自的方法、时长和结果；区分观察值、费用案例与配置或保证，不得把一个对象的事实转移给另一个。", maxChars)
	}
	messages := []ChatMessage{{Role: "system", Content: prompt}}
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
