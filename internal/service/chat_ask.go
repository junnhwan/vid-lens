package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/observability"
)

// 非流式问答编排、消息持久化和 AI 调用观测包装。
func (s *ChatService) Ask(ctx context.Context, userID, sessionID int64, question string, topK int, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile) (*AskResult, error) {
	return s.AskWithMode(ctx, ChatModeStrictRAG, userID, sessionID, question, topK, embedding, chat, profile)
}

func (s *ChatService) AskWithMode(ctx context.Context, mode ChatMode, userID, sessionID int64, question string, topK int, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile) (*AskResult, error) {
	ctx = withChatCorrelation(ctx)
	ctx = withChatExecutionRecord(ctx, mode, profile)
	_ = emitProgress(ctx, ConversationProgress{ID: "prepare", Kind: "prepare", Status: "running"})
	embedding, chat = s.observedAIClients(userID, sessionID, 0, embedding, chat, profile)
	prepared, err := s.prepareChatByMode(ctx, normalizeChatMode(mode), userID, sessionID, question, topK, embedding, chat, profile)
	if err != nil {
		return nil, err
	}
	_ = emitProgress(ctx, ConversationProgress{ID: "prepare", Kind: "prepare", Status: "done"})
	memoryPolicy := s.effectiveMemoryPolicyForRequest(ctx, prepared.Session)
	s.injectChatPreferences(ctx, prepared, memoryPolicy)
	_ = emitProgress(ctx, ConversationProgress{ID: "answer", Kind: "answer", Status: "running"})

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
			_ = emitProgress(ctx, ConversationProgress{ID: "answer", Kind: "answer", Status: "done"})
			degradationReason := "generation_unavailable"
			if prepared.DegradationReason != "" {
				degradationReason = "retrieval_and_generation_unavailable"
			}
			result, saveErr := s.saveChatExchangeWithStatus(ctx, userID, sessionID, prepared.Question, finalized.Answer, finalized.Citations, prepared.RecentLimit, profile.LLMModel, degradationReason, prepared.FrozenMemberIDs)
			if saveErr != nil {
				return nil, saveErr
			}
			result.Degraded = true
			result.MemoryPolicy = memoryPolicy
			return result, nil
		}
		// UseLLM=false 的 intent 不该走到 Chat（small_talk 占位未落地）；admission
		// RetryAfter 在阈值内由 caller 重试，此处不降级、返回错误。
		return nil, llmErr
	}

	finalized := finalizeChatAnswer(prepared, answer)
	if s.cfg.ReviewCitationSupport {
		finalized.Citations, _, err = reviewCitationSupport(ctx, chat, finalized.Answer, finalized.Citations)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	_ = emitProgress(ctx, ConversationProgress{ID: "answer", Kind: "answer", Status: "done"})
	result, err := s.saveChatExchangeWithStatus(ctx, userID, sessionID, prepared.Question, finalized.Answer, finalized.Citations, prepared.RecentLimit, profile.LLMModel, prepared.DegradationReason, prepared.FrozenMemberIDs)
	if err != nil {
		return nil, err
	}
	result.MemoryPolicy = memoryPolicy
	return result, nil
}

// finalizeChatAnswer only cleans and selects citations; it never retrieves again.
func finalizeChatAnswer(prepared *preparedRAGChat, answer string) finalizedAnswer {
	return finalizeAnswerCitations(answer, prepared.Citations)
}

func (s *ChatService) saveChatExchange(ctx context.Context, userID, sessionID int64, question, answer string, citations []Citation, recentLimit int, modelName string, frozenMembers ...[]int64) (*AskResult, error) {
	return s.saveChatExchangeWithStatus(ctx, userID, sessionID, question, answer, citations, recentLimit, modelName, "", frozenMembers...)
}

func (s *ChatService) saveChatExchangeWithStatus(ctx context.Context, userID, sessionID int64, question, answer string, citations []Citation, recentLimit int, modelName, degradationReason string, frozenMembers ...[]int64) (*AskResult, error) {
	var snapshot []byte
	var err error
	diagnosticID := ""
	if degradationReason != "" {
		diagnosticID = observability.CorrelationFromContext(ctx).TraceID
	}
	record := chatExecutionFromContext(ctx)
	mode := "chat"
	profile := ai.Profile{}
	steps := []chatExecutionStep{}
	var scope *retrievalScope
	var retrieval *RetrievalTrace
	var executionDurationMS *int64
	var executionStartedAt, executionFinishedAt *string
	if record != nil {
		scope, retrieval = record.Scope, record.Retrieval
		mode, profile = record.Mode, record.Profile
		steps = record.completedSteps()
		finished := time.Now().UTC()
		duration := finished.Sub(record.StartedAt).Milliseconds()
		if duration < 0 {
			duration = 0
		}
		startedText, finishedText := record.StartedAt.Format(time.RFC3339Nano), finished.Format(time.RFC3339Nano)
		executionDurationMS, executionStartedAt, executionFinishedAt = &duration, &startedText, &finishedText
	}
	snapshot, err = json.Marshal(struct {
		Scope               *retrievalScope     `json:"scope,omitempty"`
		Retrieval           *RetrievalTrace     `json:"retrieval,omitempty"`
		Citations           []Citation          `json:"citations"`
		Steps               []chatExecutionStep `json:"steps"`
		Mode                string              `json:"mode"`
		Degraded            bool                `json:"degraded,omitempty"`
		DegradationReason   string              `json:"degradation_reason,omitempty"`
		DiagnosticID        string              `json:"diagnostic_id,omitempty"`
		ExecutionDurationMS *int64              `json:"execution_duration_ms,omitempty"`
		ExecutionStartedAt  *string             `json:"execution_started_at,omitempty"`
		ExecutionFinishedAt *string             `json:"execution_finished_at,omitempty"`
	}{Scope: scope, Retrieval: retrieval, Citations: citations, Steps: steps, Mode: mode, Degraded: degradationReason != "", DegradationReason: degradationReason, DiagnosticID: diagnosticID, ExecutionDurationMS: executionDurationMS, ExecutionStartedAt: executionStartedAt, ExecutionFinishedAt: executionFinishedAt})
	if err != nil {
		return nil, err
	}
	if record != nil && record.Scope != nil && len(record.Scope.Ready) > 0 {
		session, err := s.repos.Chat.FindSessionForUser(userID, sessionID)
		if err != nil {
			return nil, err
		}
		if session == nil {
			return nil, errKnowledgeMembershipChanged
		}
		current, err := s.sessionRetrievalScope(userID, session, profile.EmbeddingModel)
		if err != nil {
			return nil, err
		}
		if !sameTaskIDs(current.Members, record.Scope.Members) || !sameTaskIDs(current.Ready, record.Scope.Ready) {
			return nil, errKnowledgeMembershipChanged
		}
	}
	snapshotText := string(snapshot)
	userMessage := &model.ChatMessage{SessionID: sessionID, UserID: userID, Role: "user", Content: question, ContextAnnotationsJSON: annotationJSON(ctx)}
	assistantMessage := &model.ChatMessage{SessionID: sessionID, UserID: userID, Role: "assistant", Content: answer, RetrievalSnapshot: &snapshotText, ModelName: modelName, ExecutionMode: mode, ProfileID: profile.ID}
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
	if err := s.repos.Chat.CreateExchange(userID, userMessage, assistantMessage, sourceTaskIDs, frozenMembers...); err != nil {
		return nil, err
	}

	// Presentation-only side effects happen after the durable exchange commits.
	if session, findErr := s.repos.Chat.FindSessionForUser(userID, sessionID); findErr == nil && session != nil {
		s.maybeAutoTitleSession(session, question)
	}
	if recentLimit > 0 {
		_ = s.refreshRecentMemory(ctx, userID, sessionID, recentLimit)
	}
	return &AskResult{MessageID: assistantMessage.ID, ExecutionDurationMS: executionDurationMS, Answer: answer, Citations: citations, Model: modelName, ProfileID: profile.ID, Degraded: degradationReason != "", DegradationReason: degradationReason, DiagnosticID: diagnosticID}, nil
}

func withChatCorrelation(ctx context.Context) context.Context {
	if observability.CorrelationFromContext(ctx).TraceID != "" {
		return ctx
	}
	return observability.WithCorrelation(ctx, observability.Correlation{TraceID: uuid.NewString()})
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
		JobType:   "conversation",
		Stage:     "retrieving",
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
