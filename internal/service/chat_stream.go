package service

import (
	"context"
	"fmt"

	"vid-lens/internal/ai"
)

// 流式接口适配；优先使用 provider streaming，必要时才分片已有答案。
func (s *ChatService) AskStream(ctx context.Context, userID, sessionID int64, question string, topK int, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile, emit func(ChatStreamEvent) error) (*AskResult, error) {
	return s.AskStreamWithMode(ctx, ChatModeStrictRAG, userID, sessionID, question, topK, embedding, chat, profile, emit)
}

func (s *ChatService) AskStreamWithMode(ctx context.Context, mode ChatMode, userID, sessionID int64, question string, topK int, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile, emit func(ChatStreamEvent) error) (*AskResult, error) {
	if emit == nil {
		return nil, fmt.Errorf("stream emit 不能为空")
	}
	embedding, chat = s.observedAIClients(userID, sessionID, 0, embedding, chat, profile)
	prepared, err := s.prepareChatByMode(ctx, normalizeChatMode(mode), userID, sessionID, question, topK, embedding, chat, profile)
	if err != nil {
		return nil, err
	}
	var answer string
	degraded := false
	// emitAnswer 把一段文本按流式分片发出去（非流式与档2降级共用）。
	emitAnswer := func(text string) error {
		for _, chunk := range splitAnswerForStream(text, 80) {
			if err := emit(ChatStreamEvent{Type: "answer", Data: chunk}); err != nil {
				return err
			}
		}
		return nil
	}
	// applyTier2 是档2降级入口：LLM 失败 → 无 LLM 模式（片段+摘要直拼 + degraded），
	// 不调 LLM（docs/architecture/reliability.md 稀缺点）。返回降级答案体，由 caller 发出并标 degraded。
	applyTier2 := func() error {
		degraded = true
		// 档2 不调 LLM：丢弃已累积的部分 LLM delta，用降级答案体替代
		// （docs/architecture/reliability.md 档2 = 片段+摘要直拼，不含部分 LLM 生成内容）。
		answer = s.applyTier2Degradation(ctx, prepared)
		return emitAnswer(answer)
	}
	if streaming, ok := chat.(ai.StreamingChatClient); ok {
		streamErr := streaming.StreamChat(ctx, prepared.Messages, func(delta string) error {
			answer += delta
			return emit(ChatStreamEvent{Type: "answer", Data: delta})
		})
		if streamErr != nil {
			if shouldTriggerLLMDegradation(prepared.Policy, streamErr) {
				if err := applyTier2(); err != nil {
					return nil, err
				}
			} else {
				return nil, streamErr
			}
		}
	} else {
		chatAnswer, chatErr := chat.Chat(ctx, prepared.Messages)
		if chatErr != nil {
			if shouldTriggerLLMDegradation(prepared.Policy, chatErr) {
				if err := applyTier2(); err != nil {
					return nil, err
				}
			} else {
				return nil, chatErr
			}
		} else {
			answer = chatAnswer
			if err := emitAnswer(answer); err != nil {
				return nil, err
			}
		}
	}

	// docs/architecture/retrieval.md ⑨ 轻量证据约束（与 ④ 正交：LLM 可用约束 vs 不可用降级）。见
	// applyEvidenceConstraint（Ask / AskStream 共用）。流式路径的诚实边界（spec review A2）：
	// ⑨ 是生成后校验，provider streaming 已把 delta 推给客户端，已发 delta 无法召回；
	// ⑨ 的"违规结论被拒"落在持久化层——DB 存约束后版本、citations 事件发约束后引用，
	// 不假装能回收已发 token。applyEvidenceConstraint 已内含 finalizeAnswerCitations，
	// 此处不再二次 finalize（二次 finalize 会让已去 token 的答案回退到 fallback citation）。
	constrained := s.applyEvidenceConstraint(ctx, prepared, answer)
	result, err := s.saveChatExchange(ctx, userID, sessionID, prepared.Question, constrained.answer, constrained.citations, prepared.RecentLimit, profile.LLMModel)
	if err != nil {
		return nil, err
	}
	result.Degraded = degraded
	if err := emit(ChatStreamEvent{Type: "citations", Data: constrained.citations}); err != nil {
		return nil, err
	}
	if err := emit(ChatStreamEvent{Type: "done", Data: map[string]interface{}{
		"message_id": result.MessageID,
		"model":      result.Model,
		"answer":     result.Answer,
		"degraded":   degraded,
	}}); err != nil {
		return nil, err
	}
	return result, nil
}

func splitAnswerForStream(answer string, maxRunes int) []string {
	if maxRunes <= 0 {
		maxRunes = 80
	}
	runes := []rune(answer)
	if len(runes) == 0 {
		return []string{""}
	}
	parts := make([]string, 0, (len(runes)/maxRunes)+1)
	for len(runes) > 0 {
		n := maxRunes
		if len(runes) < n {
			n = len(runes)
		}
		parts = append(parts, string(runes[:n]))
		runes = runes[n:]
	}
	return parts
}
