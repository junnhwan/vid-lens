package service

import (
	"fmt"
	"strings"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

// BuildRAGAnswerMessages builds the ordinary RAG answer prompt without chat memory.
// Eval tooling uses this so it can score answers without importing chat internals.
func BuildRAGAnswerMessages(citations []RetrievedChunk, question string) []ai.ChatMessage {
	return buildRAGMessages(citations, nil, question)
}

// RAG/视频助手 prompt 消息构造和轻量文本判断。
func buildRAGMessages(contexts []RetrievedChunk, recent []model.ChatMessage, question string) []ai.ChatMessage {
	contextLines := make([]string, 0, len(contexts))
	for index, chunk := range contexts {
		contextLines = append(contextLines, fmt.Sprintf("[C%d]\n%s\n%s", index+1, describeRetrievedChunk(chunk), chunk.Content))
	}

	messages := []ai.ChatMessage{
		{
			Role:    "system",
			Content: "你是 VidLens 的视频内容问答助手。你只能基于给定的视频片段和必要的会话上下文回答。如果检索片段中没有答案，直接说明当前视频片段中没有找到相关信息，不要编造。证据编号是内部标记。只引用支撑回答所必需的最小充分证据，不要为了增加引用数量而标注重复或无关片段。回答涉及具体事实时，请在对应事实后使用独立格式 [C1][C2] 标注证据，不要写成 [C1, C2]。系统会在展示前隐藏这些标记。",
		},
		{
			Role:    "system",
			Content: "检索到的视频片段：\n" + strings.Join(contextLines, "\n\n"),
		},
	}
	for _, msg := range recent {
		if msg.Role == "user" || msg.Role == "assistant" {
			messages = append(messages, ai.ChatMessage{Role: msg.Role, Content: msg.Content})
		}
	}
	messages = append(messages, ai.ChatMessage{Role: "user", Content: question})
	return messages
}

func buildVideoAssistantMessages(videoContext string, recent []model.ChatMessage, question string) []ai.ChatMessage {
	messages := []ai.ChatMessage{
		{
			Role:    "system",
			Content: "你是 VidLens 的视频助手。优先基于提供的视频摘要和转写回答。可以做整体概括、解释和延伸，但不能把未提供的信息说成来自视频。如果用户问题明显和视频无关，可以正常回答，并明确说明这部分不基于当前视频内容。",
		},
		{
			Role:    "system",
			Content: "可用的视频上下文：\n" + videoContext,
		},
	}
	for _, msg := range recent {
		if msg.Role == "user" || msg.Role == "assistant" {
			messages = append(messages, ai.ChatMessage{Role: msg.Role, Content: msg.Content})
		}
	}
	messages = append(messages, ai.ChatMessage{Role: "user", Content: question})
	return messages
}

func isVideoOverviewQuestion(question string) bool {
	q := strings.TrimSpace(strings.ToLower(question))
	if q == "" {
		return false
	}
	overviewHints := []string{
		"讲了什么",
		"说了什么",
		"主要内容",
		"核心内容",
		"核心观点",
		"主要观点",
		"视频概括",
		"视频概览",
		"总结一下",
		"简单总结",
		"简要总结",
		"简要讲",
		"概括一下",
		"归纳一下",
		"overview",
		"summary",
		"summarize",
	}
	for _, hint := range overviewHints {
		if strings.Contains(q, hint) {
			return true
		}
	}
	return false
}

func trimRunes(text string, maxRunes int) string {
	if maxRunes <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "\n\n[已截断，仅提供前半部分上下文]"
}
