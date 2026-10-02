package service

import (
	"strings"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

const sentenceCitationProductPrompt = "每个 [Cn] 仅指向编号后的一句原文，不代表整段上下文。选择直接支撑该具体观点的原文句子；同一回答涉及不同句子或不同时间点时，分别引用对应编号。扩展上下文帮助理解，不能借某句编号引用上下文中的其他事实；没有对应句子依据时明确未能确认。只使用本轮候选编号，不沿用历史回答中的编号。"
const ragProductPrompt = "你是 VidLens 的视频内容问答助手。结合视频片段和会话上下文自然回答。一般知识可以解释，但应与视频事实区分。如果片段没有依据，明确无法从视频确认，不要编造视频事实或引用。每条证据都标有 modality 和半开时间范围；回答具体事实时必须引用这些带模态和时间的证据。若 transcript、visual_ocr、visual_caption 互相冲突，不得把它们合并成单一确定事实，必须分别说明各模态观察并明确不确定性。证据编号用于行内引用链接。只引用支撑回答所必需的最小充分证据，不要为了增加引用数量而标注重复或无关片段。回答涉及具体事实时，请在对应事实后使用独立格式 [C1][C2] 标注证据，不要写成 [C1, C2]。" + sentenceCitationProductPrompt
const videoAssistantProductPrompt = "你是 VidLens 的视频助手。优先基于提供的视频摘要和转写回答。可以做整体概括、解释和延伸，但不能把未提供的信息说成来自视频。如果用户问题明显和视频无关，可以正常回答，并明确说明这部分不基于当前视频内容。"

func ChatProductInstructions() string {
	return qaGroundingPolicy + "\n\n检索问答：\n" + ragProductPrompt + "\n\n视频概览：\n" + videoAssistantProductPrompt
}

// BuildRAGAnswerMessages builds the ordinary RAG answer prompt without chat memory.
// Eval tooling uses this so it can score answers without importing chat internals.
func BuildRAGAnswerMessages(citations []RetrievedChunk, question string) []ai.ChatMessage {
	return buildRAGMessages(citations, nil, question)
}

// RAG/视频助手 prompt 消息构造和轻量文本判断。
func buildRAGMessages(contexts []RetrievedChunk, recent []model.ChatMessage, question string) []ai.ChatMessage {
	contexts, citations := buildCitationSet(question, contexts)

	messages := []ai.ChatMessage{
		{
			Role:    "system",
			Content: ragProductPrompt + "\n" + qaGroundingPolicy + "\n" + qaRuntimeClock(time.Now()),
		},
		{
			Role:    "user",
			Content: "可引用的原文句子：\n" + formatCitationCandidates(citations) + "\n\n扩展上下文（仅供理解，无独立引用编号）：\n" + formatCitationGenerationContext(contexts),
		},
	}
	for _, msg := range boundedConversationContext(recent) {
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
			Content: videoAssistantProductPrompt + "\n" + qaGroundingPolicy + "\n" + qaRuntimeClock(time.Now()) + "\n当前上下文没有可引用编号，不得生成 [Cn]；摘要属于衍生内容，有转写时优先核对转写，只有摘要时说明依据为摘要。",
		},
		{
			Role:    "user",
			Content: "可用的视频上下文：\n" + videoContext,
		},
	}
	for _, msg := range boundedConversationContext(recent) {
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
