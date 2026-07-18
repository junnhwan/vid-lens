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
	if streaming, ok := chat.(ai.StreamingChatClient); ok {
		err = streaming.StreamChat(ctx, prepared.Messages, func(delta string) error {
			answer += delta
			return emit(ChatStreamEvent{Type: "answer", Data: delta})
		})
		if err != nil {
			return nil, err
		}
	} else {
		answer, err = chat.Chat(ctx, prepared.Messages)
		if err != nil {
			return nil, err
		}
		for _, chunk := range splitAnswerForStream(answer, 80) {
			if err := emit(ChatStreamEvent{Type: "answer", Data: chunk}); err != nil {
				return nil, err
			}
		}
	}

	finalized := finalizeAnswerCitations(answer, prepared.Citations)
	result, err := s.saveChatExchange(ctx, userID, sessionID, prepared.Question, finalized.Answer, finalized.Citations, prepared.RecentLimit, profile.LLMModel)
	if err != nil {
		return nil, err
	}
	if err := emit(ChatStreamEvent{Type: "citations", Data: finalized.Citations}); err != nil {
		return nil, err
	}
	if err := emit(ChatStreamEvent{Type: "done", Data: map[string]interface{}{
		"message_id": result.MessageID,
		"model":      result.Model,
		"answer":     result.Answer,
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
