package service

import (
	"context"
	"encoding/json"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

// 非流式问答编排、消息持久化和 AI 调用观测包装。
func (s *ChatService) Ask(ctx context.Context, userID, sessionID int64, question string, topK int, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile) (*AskResult, error) {
	return s.AskWithMode(ctx, ChatModeStrictRAG, userID, sessionID, question, topK, embedding, chat, profile)
}

func (s *ChatService) AskWithMode(ctx context.Context, mode ChatMode, userID, sessionID int64, question string, topK int, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile) (*AskResult, error) {
	embedding, chat = s.observedAIClients(userID, sessionID, 0, embedding, chat, profile)
	prepared, err := s.prepareChatByMode(ctx, normalizeChatMode(mode), userID, sessionID, question, topK, embedding, chat, profile)
	if err != nil {
		return nil, err
	}

	answer, err := chat.Chat(ctx, prepared.Messages)
	if err != nil {
		return nil, err
	}

	finalized := finalizeAnswerCitations(answer, prepared.Citations)
	return s.saveChatExchange(ctx, userID, sessionID, prepared.Question, finalized.Answer, finalized.Citations, prepared.RecentLimit, profile.LLMModel)
}

func (s *ChatService) saveChatExchange(ctx context.Context, userID, sessionID int64, question, answer string, citations []Citation, recentLimit int, modelName string) (*AskResult, error) {
	snapshot, err := json.Marshal(citations)
	if err != nil {
		return nil, err
	}
	snapshotText := string(snapshot)
	userMessage := &model.ChatMessage{SessionID: sessionID, UserID: userID, Role: "user", Content: question}
	assistantMessage := &model.ChatMessage{SessionID: sessionID, UserID: userID, Role: "assistant", Content: answer, RetrievalSnapshot: &snapshotText, ModelName: modelName}
	sourceTaskIDs := make([]int64, 0, len(citations))
	seenTasks := make(map[int64]struct{}, len(citations))
	for _, citation := range citations {
		if citation.TaskID <= 0 {
			continue
		}
		if _, ok := seenTasks[citation.TaskID]; ok {
			continue
		}
		seenTasks[citation.TaskID] = struct{}{}
		sourceTaskIDs = append(sourceTaskIDs, citation.TaskID)
	}
	if err := s.repos.Chat.CreateExchange(userID, userMessage, assistantMessage, sourceTaskIDs); err != nil {
		return nil, err
	}

	// Presentation-only side effects happen after the durable exchange commits.
	if session, findErr := s.repos.Chat.FindSessionForUser(userID, sessionID); findErr == nil && session != nil {
		s.maybeAutoTitleSession(session, question)
	}
	_ = s.refreshRecentMemory(ctx, userID, sessionID, recentLimit)
	return &AskResult{MessageID: assistantMessage.ID, Answer: answer, Citations: citations, Model: modelName}, nil
}

func (s *ChatService) observedAIClients(userID, sessionID, taskID int64, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile) (ai.EmbeddingClient, ai.ChatClient) {
	if s.recorder == nil {
		return embedding, chat
	}
	if taskID <= 0 && sessionID > 0 {
		session, err := s.repos.Chat.FindSessionForUser(userID, sessionID)
		if err == nil && session != nil {
			taskID = session.TaskID
		}
	}
	embedding = ai.NewObservedEmbeddingClient(embedding, s.recorder, ai.CallContext{
		UserID:    userID,
		TaskID:    taskID,
		SessionID: sessionID,
		Provider:  profile.EmbeddingProvider,
		Model:     profile.EmbeddingModel,
	})
	chat = ai.NewObservedChatClient(chat, s.recorder, ai.CallContext{
		UserID:    userID,
		TaskID:    taskID,
		SessionID: sessionID,
		Provider:  profile.LLMProvider,
		Model:     profile.LLMModel,
	})
	return embedding, chat
}
