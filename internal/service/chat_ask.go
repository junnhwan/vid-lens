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

	answer, llmErr := chat.Chat(ctx, prepared.Messages)
	if llmErr != nil {
		// docs/architecture/reliability.md 档2：LLM 失败 → 无 LLM 模式。该走 LLM 但 LLM 挂了 → 回退检索片段
		// + 已有摘要直拼 + degraded:true，不调 LLM（当前实现约束稀缺点）。
		// 前置诚信检查：无 LLM 模式 = buildRAGMessages 降级补全，非从零新建——
		// 片段拼装已有（prepared.Contexts / prepared.Messages），此处只补"不调 LLM +
		// degraded 标志 + 复用 docs/architecture/data-model.md FindByMD5 摘要"路径。
		if shouldTriggerLLMDegradation(prepared.Policy, llmErr) {
			degradedAnswer := s.applyTier2Degradation(ctx, prepared)
			finalized := finalizeAnswerCitations(degradedAnswer, prepared.Citations)
			result, saveErr := s.saveChatExchange(ctx, userID, sessionID, prepared.Question, finalized.Answer, finalized.Citations, prepared.RecentLimit, profile.LLMModel)
			if saveErr != nil {
				return nil, saveErr
			}
			result.Degraded = true
			return result, nil
		}
		// UseLLM=false 的 intent 不该走到 Chat（small_talk 占位未落地）；admission
		// RetryAfter 在阈值内由 caller 重试，此处不降级、返回错误。
		return nil, llmErr
	}

	// docs/architecture/retrieval.md ⑨ 轻量证据约束（LLM 可用时的约束，与 ④ LLM 不可用降级正交）：见
	// applyEvidenceConstraint（Ask / AskStream 共用，消除两处重复 closure）。
	constrained := s.applyEvidenceConstraint(ctx, prepared, answer)
	result, err := s.saveChatExchange(ctx, userID, sessionID, prepared.Question, constrained.answer, constrained.citations, prepared.RecentLimit, profile.LLMModel)
	if err != nil {
		return nil, err
	}
	// 档1（rerank 失败→向量基线）不标 degraded（LLM 仍生成完整答案），但已被
	// rag_pipeline 计一次档1触发。此处无需再标。
	return result, nil
}

// applyEvidenceConstraint 是 Ask / AskStream 共用的 docs/architecture/retrieval.md ⑨ 证据约束入口
// （代码复用约束 — 两处重复的 closure + disclaimer 合并此处）。
// enforceEvidenceConstraint 已内含 finalizeAnswerCitations（无违规走标准清洗，违规
// 走重检索补证据或"无证据支撑"标注），caller 不再二次 finalize——二次 finalize 会把
// 已去掉引用 token 的答案再 finalize 一遍导致引用集回退到 fallback（docs/architecture/retrieval.md）。
func (s *ChatService) applyEvidenceConstraint(ctx context.Context, prepared *preparedRAGChat, answer string) evidenceConstraintOutcome {
	return s.enforceEvidenceConstraint(ctx, prepared, answer, func(ctx context.Context, query string) ([]RetrievedChunk, error) {
		return s.reretrieveEvidence(ctx, prepared, query)
	})
}

// reretrieveEvidence 是 docs/architecture/retrieval.md 证据约束的重检索入口：以违规结论涉及的 query
// 复用 docs/architecture/retrieval.md 检索链路（newRetrievalPipeline + Retrieve）补证据。
//
// 轻量边界（当前实现约束）：单轮重检索，caller enforceEvidenceConstraint 已用
// maxEvidenceReRetrieval=1 上限保证不无限循环。这里只负责"以给定 query 在原
// session 的 task 范围重检索一次"。复用 prepared.TaskIDs / EmbeddingModel，
// 不重建检索基础设施（docs/architecture/retrieval.md 当前实现约束"复用现有 seam"）。
//
// 返回的 RetrievedChunk 由 enforceEvidenceConstraint 经 buildCitationSet 并入候选集。
func (s *ChatService) reretrieveEvidence(ctx context.Context, prepared *preparedRAGChat, query string) ([]RetrievedChunk, error) {
	if s == nil || prepared == nil {
		return nil, nil
	}
	taskIDs := prepared.TaskIDs
	if len(taskIDs) == 0 && prepared.Session != nil && prepared.Session.TaskID > 0 {
		taskIDs = []int64{prepared.Session.TaskID}
	}
	if len(taskIDs) == 0 {
		return nil, nil
	}
	pipeline := s.newRetrievalPipeline(prepared.TopK, prepared.ChatClient, ai.Profile{EmbeddingModel: prepared.EmbeddingModel})
	pipeline.applyPolicy(prepared.Policy)
	retrieval, err := pipeline.Retrieve(ctx, RetrievalPipelineRequest{
		UserID:         prepared.Session.UserID,
		TaskIDs:        taskIDs,
		Question:       query,
		TopK:           prepared.TopK,
		EmbeddingModel: prepared.EmbeddingModel,
		Embedding:      prepared.EmbeddingClient,
	})
	if err != nil {
		return nil, err
	}
	return retrieval.Citations, nil
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
	if recentLimit > 0 {
		_ = s.refreshRecentMemory(ctx, userID, sessionID, recentLimit)
	}
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
