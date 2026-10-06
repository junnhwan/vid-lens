package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

// All new Chat requests use natural video context or member-scoped KB retrieval.
func normalizeChatMode(mode ChatMode) ChatMode {
	return ChatModeNatural
}

func (s *ChatService) prepareChatByMode(ctx context.Context, mode ChatMode, userID, sessionID int64, question string, topK int, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile) (*preparedRAGChat, error) {
	session, err := s.repos.Chat.FindSessionForUser(userID, sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, fmt.Errorf("无权访问此会话")
	}
	// KnowledgeBase 会话强制走 RAG（跨视频检索，集合 scope），与 strict_rag 同路径。
	if session.ScopeType == model.ChatScopeKnowledgeBase || session.ScopeType == model.ChatScopeVideoLibrary {
		return s.prepareRAGChat(ctx, mode, userID, sessionID, question, topK, embedding, chat, profile)
	}
	return s.prepareVideoAssistantChat(ctx, mode, userID, sessionID, question, topK, embedding, chat, profile)
}

func (s *ChatService) prepareRAGChat(ctx context.Context, mode ChatMode, userID, sessionID int64, question string, topK int, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile) (*preparedRAGChat, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, fmt.Errorf("问题不能为空")
	}
	if len([]rune(question)) > 1000 {
		return nil, fmt.Errorf("问题过长")
	}

	session, err := s.repos.Chat.FindSessionForUser(userID, sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, fmt.Errorf("无权访问此会话")
	}
	if s.retriever == nil {
		return nil, errRAGIndexUnavailable
	}
	scope, err := s.sessionRetrievalScope(userID, session, profile.EmbeddingModel)
	taskIDs := scope.Ready
	if err != nil {
		return nil, err
	}

	// Share authorized recent history across Chat and Agent before retrieval.
	recentLimit := s.cfg.RecentTurns * 2
	recent, err := s.loadScopeSafeRecentMessages(ctx, userID, session, taskIDs, recentLimit)
	if err != nil {
		return nil, err
	}
	if session.ScopeType == model.ChatScopeKnowledgeBase || session.ScopeType == model.ChatScopeVideoLibrary {
		recentLimit = 0
	} // KB never publishes provenance-free Redis history.

	intent := s.classifyIntent(ctx, question, session, mode, recent, chat)
	policy := PolicyFor(intent, scopeOfSession(session))
	if !policy.Retrieve {
		// strict_rag / KB 路径要求检索；policy.Retrieve=false 在此分支理论上不发生
		// （分类器对 KB/strict 不产出 overview/small_talk 关检索）。防御性兜底。
		return nil, errNoRetrievedContext
	}
	// 散落判定 1（topK 默认值 + topK>10→10 上限）由 ExecutionPolicy.ClampTopK
	// 统一表达（docs/architecture/retrieval.md 待评测指标 A段）。
	topK = policy.ClampTopK(topK)
	if topK <= 0 {
		topK = s.cfg.TopK
	}

	var route collectionRoute
	if scopeOfSession(session) == ScopeCollection {
		route, err = s.routeCollection(ctx, userID, taskIDs, question, chat)
		if err != nil {
			return nil, err
		}
	}
	pipeline, err := s.newUserRetrievalPipeline(ctx, userID, topK, chat, profile)
	if err != nil {
		return nil, err
	}
	policy.Rerank = policy.Rerank && pipeline.reranker != nil
	// 知识库混合检索（EnableVector=true/EnableBM25=true）由
	// policy.Scope==collection 统一表达；rerank 开关由 policy.Rerank 映射。
	pipeline.applyPolicy(policy)
	var timeRanges []TimestampRange
	if intent == IntentTimelineLocate {
		timeRanges = ExtractSignals(question).Timestamps
	}

	if err := emitProgress(ctx, ConversationProgress{ID: "retrieve", Kind: "retrieve", Label: "检索视频证据", Status: "running"}); err != nil {
		return nil, err
	}
	retrieval, err := pipeline.Retrieve(ctx, RetrievalPipelineRequest{
		UserID:          userID,
		TaskIDs:         taskIDs,
		Question:        question,
		Recent:          recent,
		TopK:            topK,
		EmbeddingModel:  profile.EmbeddingModel,
		Embedding:       embedding,
		TimeRanges:      timeRanges,
		RequiredTaskIDs: route.Required, RoutedTaskIDs: route.routedIDs(), Dimensions: route.Dimensions,
	})
	if err != nil {
		_ = emitProgress(ctx, ConversationProgress{ID: "retrieve", Kind: "retrieve", Label: "检索未完成", Status: "error", Detail: "检索未完成"})
		return nil, err
	}
	if record := chatExecutionFromContext(ctx); record != nil {
		record.Retrieval = &retrieval.Trace
		record.Scope = &scope
	}
	contexts, citations := buildCitationSet(question, retrieval.Citations)
	if err := emitProgress(ctx, ConversationProgress{ID: "retrieve", Kind: "retrieve", Label: "检索完成", Status: "done", Detail: fmt.Sprintf("找到 %d 条候选引用", len(citations))}); err != nil {
		return nil, err
	}

	messages := buildRAGMessages(contexts, recent, question)
	if scopeOfSession(session) == ScopeCollection {
		messages = append([]ai.ChatMessage{{Role: "system", Content: scope.coveragePrompt() + "\n" + collectionRoutePrompt(route)}}, messages...)
	}
	if s.repos.AIProfile != nil {
		preference, err := s.repos.AIProfile.PromptPreference(userID, "chat")
		if err != nil {
			return nil, err
		}
		messages = appendUserPromptPreference(messages, preference)
	}
	if session.ScopeType == model.ChatScopeKnowledgeBase || session.ScopeType == model.ChatScopeVideoLibrary {
		messages = append([]ai.ChatMessage{{Role: "system", Content: evidenceCoveragePrompt(taskIDs, retrieval.Citations)}}, messages...)
	}
	if session.ScopeType != model.ChatScopeKnowledgeBase && session.ScopeType != model.ChatScopeVideoLibrary {
		contextText, contextErr := s.videoContextText(session.UserID, session.TaskID)
		if contextErr != nil {
			return nil, contextErr
		}
		messages = append([]ai.ChatMessage{messages[0], {Role: "user", Content: "有限视频上下文（不是可引用片段）：\n" + contextText}}, messages[1:]...)
	}
	return &preparedRAGChat{
		FrozenMemberIDs: append([]int64(nil), scope.Members...),
		Session:         session,
		Question:        question,
		TopK:            topK,
		RecentLimit:     recentLimit,
		Contexts:        contexts,
		Citations:       citations,
		Messages:        messages,
		Policy:          policy,
	}, nil
}

func (s *ChatService) prepareVideoAssistantChat(ctx context.Context, mode ChatMode, userID, sessionID int64, question string, topK int, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile) (*preparedRAGChat, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, fmt.Errorf("问题不能为空")
	}
	if len([]rune(question)) > 1000 {
		return nil, fmt.Errorf("问题过长")
	}

	session, err := s.repos.Chat.FindSessionForUser(userID, sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, fmt.Errorf("无权访问此会话")
	}

	recentLimit := s.cfg.RecentTurns * 2
	recent, err := s.loadRecentMessages(ctx, userID, sessionID, recentLimit)
	if err != nil {
		return nil, err
	}

	// docs/architecture/retrieval.md：级联分类器替换占位；recent 已加载，传历史 intent 加权。
	intent := s.classifyIntent(ctx, question, session, mode, recent, chat)
	policy := PolicyFor(intent, scopeOfSession(session))
	if !policy.Retrieve {
		return s.prepareVideoContextChat(session, question, recent, recentLimit)
	}

	prepared, ragErr := s.prepareRAGChat(ctx, mode, userID, sessionID, question, topK, embedding, chat, profile)
	if ragErr == nil {
		return prepared, nil
	}
	// 视频助手应在检索链路不可用时继续使用已校验会话的摘要/转写，
	// 但客户端取消或请求超时后不能再发起兜底模型调用。
	if errors.Is(ragErr, context.Canceled) || errors.Is(ragErr, context.DeadlineExceeded) {
		return nil, ragErr
	}
	prepared, err = s.prepareVideoContextChat(session, question, recent, recentLimit)
	if err != nil {
		return nil, err
	}
	prepared.DegradationReason = "retrieval_unavailable"
	prepared.Messages[0].Content += "\n" + qaRetrievalUnavailablePrompt
	_ = emitProgress(ctx, ConversationProgress{ID: "fallback", Kind: "retrieve", Label: "已改用摘要 / 转写回答，无检索引用", Status: "done"})
	return prepared, nil
}

func (s *ChatService) prepareVideoContextChat(session *model.ChatSession, question string, recent []model.ChatMessage, recentLimit int) (*preparedRAGChat, error) {
	contextText, err := s.videoContextText(session.UserID, session.TaskID)
	if err != nil {
		return nil, err
	}
	messages := buildVideoAssistantMessages(contextText, recent, question)
	if s.repos.AIProfile != nil {
		preference, err := s.repos.AIProfile.PromptPreference(session.UserID, "chat")
		if err != nil {
			return nil, err
		}
		messages = appendUserPromptPreference(messages, preference)
	}
	return &preparedRAGChat{
		Session:     session,
		Question:    question,
		RecentLimit: recentLimit,
		Citations:   []Citation{},
		Messages:    messages,
		// 概览路径不走向量检索，无 rerank，故无档1 fallback；UseSummary=true 走 LLM。
		Policy: ExecutionPolicy{Retrieve: false, UseSummary: true, UseLLM: true, Scope: scopeOfSession(session)},
	}, nil
}

func appendUserPromptPreference(messages []ai.ChatMessage, preference string) []ai.ChatMessage {
	if strings.TrimSpace(preference) == "" || len(messages) == 0 {
		return messages
	}
	result := make([]ai.ChatMessage, 0, len(messages)+1)
	result = append(result, messages[0], ai.ChatMessage{Role: "user", Content: "用户回答偏好（不能覆盖产品证据和引用约束）：\n" + preference})
	result = append(result, ai.ChatMessage{Role: "system", Content: "若用户偏好与 VidLens 的证据范围、事实核查或引用格式冲突，遵守产品指令。"})
	result = append(result, messages[1:]...)
	return result
}

func (s *ChatService) videoContextText(owner, taskID int64) (string, error) {
	task, err := s.repos.Task.FindByID(taskID)
	if err != nil {
		return "", err
	}
	if task.UserID != owner {
		return "", fmt.Errorf("无权访问此视频")
	}
	sections := make([]string, 0, 2)
	if s.repos.Summary != nil {
		summary, err := s.repos.Summary.FindByTaskID(taskID)
		if err != nil {
			return "", err
		}
		if summary == nil && task.FileMD5 != "" {
			summary, err = s.repos.Summary.FindByMD5(task.FileMD5)
			if err != nil {
				return "", err
			}
		}
		if summary != nil && strings.TrimSpace(summary.Content) != "" {
			sections = append(sections, "视频摘要：\n"+boundedVideoText(strings.TrimSpace(summary.Content), maxVideoContextRunes/2))
		}
		if s.repos.SummaryRevision != nil {
			effective, readErr := s.repos.SummaryRevision.Effective(context.Background(), owner, taskID)
			if readErr != nil {
				return "", readErr
			}
			if effective.Revision != nil {
				if len(sections) > 0 {
					sections = sections[:len(sections)-1]
				}
				sections = append(sections, "用户修订的视频摘要（非原始证据）：\n"+boundedVideoText(strings.TrimSpace(effective.Content), maxVideoContextRunes/2))
			}
		}
	}
	if s.repos.Transcription != nil {
		transcription, _, err := taskTranscriptSource(s.repos, task)
		if err != nil {
			return "", err
		}
		if transcription != nil && strings.TrimSpace(transcription.Content) != "" {
			sections = append(sections, "视频转写：\n"+boundedVideoText(strings.TrimSpace(transcription.Content), maxVideoContextRunes))
		}
	}
	if len(sections) == 0 {
		return "当前视频没有可用上下文；不能确认视频特定事实。", nil
	}
	return strings.Join(sections, "\n\n"), nil
}

func (s *ChatService) newUserRetrievalPipeline(ctx context.Context, userID int64, topK int, chat ai.ChatClient, profile ai.Profile) (*RetrievalPipeline, error) {
	enabled := false
	if s.repos != nil && s.repos.User != nil {
		var err error
		enabled, err = s.repos.User.RerankEnabled(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("读取检索重排偏好失败: %w", err)
		}
	}
	return s.newRetrievalPipeline(topK, chat, profile, enabled), nil
}

func (s *ChatService) newRetrievalPipeline(topK int, chat ai.ChatClient, profile ai.Profile, authorization ...bool) *RetrievalPipeline {
	cfg := s.cfg.Retrieval
	if strings.TrimSpace(profile.RerankModel) != "" {
		copy := DefaultRAGRetrievalConfig()
		if cfg != nil {
			copy = *cfg
		}
		copy.RerankerMode = RerankerModeModel
		copy.RerankerVersion = profile.RerankModel
		cfg = &copy
	}
	if len(authorization) > 0 && !authorization[0] {
		copy := DefaultRAGRetrievalConfig()
		if cfg != nil {
			copy = *cfg
		} else {
			// Materializing the legacy nil-config path must preserve its request
			// limits; disabling rerank must not expand the candidate pool.
			if topK <= 0 {
				topK = 5
			}
			copy.TopK, copy.CandidateK, copy.MinVectorScore = topK, s.candidateK(topK), s.cfg.MinScore
		}
		copy.RerankerMode, copy.RerankerVersion = RerankerModeNone, ""
		cfg = &copy
	}
	var rewriter QueryRewriter = NewLLMQueryRewriter(chat)
	var expander *ContextExpander
	if cfg == nil {
		expander = &ContextExpander{repos: s.repos, Radius: 1, MaxCharsPerCitation: 4000}
	} else {
		switch cfg.QueryMode {
		case QueryModeOriginal:
			rewriter = NoopQueryRewriter{}
		case QueryModePreprocess:
			rewriter = PreprocessQueryRewriter{}
		case QueryModeRewrite:
			rewriter = NewLLMQueryRewriter(chat)
		}
		if cfg.NeighborRadius > 0 {
			expander = &ContextExpander{repos: s.repos, Radius: cfg.NeighborRadius, MaxCharsPerCitation: cfg.MaxContextChars}
		}
	}
	var reranker Reranker
	if cfg == nil || cfg.RerankerMode == RerankerModeDeterministic {
		reranker = DeterministicReranker{}
	} else if cfg.RerankerMode == RerankerModeModel && s.cfg.ModelRerankerFactory != nil {
		profile.RerankModel = cfg.RerankerVersion
		reranker = s.cfg.ModelRerankerFactory(profile)
	}
	return &RetrievalPipeline{repos: s.repos, retriever: s.retriever, rewriter: rewriter, expander: expander,
		RequestCache: newRetrievalRequestCache(), reranker: reranker, CandidateK: s.candidateK(topK), MinScore: s.cfg.MinScore, Config: cfg}
}

func (s *ChatService) candidateK(topK int) int {
	candidateK := s.cfg.CandidateK
	if candidateK <= 0 {
		return topK
	}
	if candidateK < topK {
		return topK
	}
	if candidateK > 50 {
		return 50
	}
	return candidateK
}

func retrievalChunkKey(chunk RetrievedChunk) string {
	if evidenceID := strings.TrimSpace(chunk.EvidenceID); evidenceID != "" {
		return fmt.Sprintf("task:%d:evidence:%s", chunk.TaskID, evidenceID)
	}
	if chunk.ChunkID > 0 {
		return fmt.Sprintf("task:%d:id:%d", chunk.TaskID, chunk.ChunkID)
	}
	return fmt.Sprintf("task:%d:idx:%d:%s", chunk.TaskID, chunk.ChunkIndex, chunk.Content)
}

// classifyIntent 是 docs/architecture/retrieval.md intent 分类入口：优先走 IntentRouter 级联（规则层
// 短路 + LLM 兜底），router 为 nil 时降级占位 classifyIntentPlaceholder（保测试
// 稳定，当前实现约束）。recent 用于历史 intent 加权 + LLM 兜底消歧指代。
func (s *ChatService) classifyIntent(ctx context.Context, question string, session *model.ChatSession, mode ChatMode, recent []model.ChatMessage, chat ai.ChatClient) Intent {
	var recentIntents []Intent
	if s.intentRouter != nil {
		recentIntents = s.intentRouter.ParseRecentIntents(recent, session, mode)
	}
	intent, _ := NewRuleIntentClassifier().Classify(question, session, mode, recentIntents)
	return intent
}

type retrievalScope struct {
	Members     []int64 `json:"members"`
	Ready       []int64 `json:"ready"`
	Unavailable []int64 `json:"unavailable"`
}

func (s *ChatService) sessionRetrievalTaskIDs(userID int64, session *model.ChatSession, embeddingModel string) ([]int64, error) {
	scope, err := s.sessionRetrievalScope(userID, session, embeddingModel)
	if err != nil {
		return nil, err
	}
	if len(scope.Unavailable) > 0 {
		return nil, s.unavailableScopeError(userID, scope.Unavailable)
	}
	return scope.Ready, nil
}

// Ownership and membership are distinct from current-model index availability.
// Unready members produce explicit partial coverage, never a widened scope.
func (s *ChatService) sessionRetrievalScope(userID int64, session *model.ChatSession, embeddingModel string) (retrievalScope, error) {
	var scope retrievalScope
	if scopeOfSession(session) != ScopeCollection {
		if session.TaskID <= 0 {
			return scope, fmt.Errorf("视频会话缺少 task_id")
		}
		scope.Members = []int64{session.TaskID}
		scope.Ready = scope.Members
		return scope, nil
	}
	var ids []int64
	var err error
	if session.ScopeType == model.ChatScopeVideoLibrary {
		ids, err = s.repos.Task.ListOwnedTaskIDs(userID)
	} else {
		kb, err := s.repos.KnowledgeBase.FindByIDForUser(userID, session.KnowledgeBaseID)
		if err != nil {
			return scope, err
		}
		if kb == nil {
			return scope, fmt.Errorf("知识库不存在或无权限")
		}
		ids, err = s.repos.KnowledgeBase.ListMembershipTaskIDsForUser(userID, session.KnowledgeBaseID)
	}
	if err != nil {
		return scope, err
	}
	ids = normalizeTaskIDs(ids)
	if len(ids) == 0 {
		return scope, fmt.Errorf("集合没有可检索视频")
	}
	tasks, err := s.repos.Task.ListByIDsForUser(userID, ids)
	if err != nil {
		return scope, err
	}
	visibleTasks := make(map[int64]model.VideoTask, len(tasks))
	for _, task := range tasks {
		visibleTasks[task.ID] = task
		scope.Members = append(scope.Members, task.ID)
	}
	indexes, err := s.repos.RAGIndex.ListByTaskIDsAndModel(userID, ids, embeddingModel)
	if err != nil {
		return scope, err
	}
	ready := map[int64]bool{}
	for _, index := range indexes {
		if index.Status == model.RAGIndexStatusIndexed {
			ready[index.TaskID] = true
		}
	}
	for _, id := range ids {
		if _, visible := visibleTasks[id]; visible && ready[id] {
			scope.Ready = append(scope.Ready, id)
		} else {
			scope.Unavailable = append(scope.Unavailable, id)
		}
	}
	if len(scope.Ready) == 0 {
		return scope, s.unavailableScopeError(userID, scope.Unavailable)
	}
	return scope, nil
}

func (scope retrievalScope) coveragePrompt() string {
	return fmt.Sprintf("集合总成员=%d；当前模型可检索=%d；未就绪或不可用成员=%d。实际回答范围只能覆盖本次可检索成员；未就绪成员未被搜索，不能说其未讨论某主题。%s", len(scope.Members), len(scope.Ready), len(scope.Unavailable), func() string {
		if len(scope.Unavailable) > 0 {
			return "本次是部分结果，请在回答中明确限制；不能称完整全库概览或完整比较。"
		}
		return ""
	}())
}

func normalizeTaskIDs(taskIDs []int64) []int64 {
	seen := make(map[int64]struct{}, len(taskIDs))
	normalized := make([]int64, 0, len(taskIDs))
	for _, id := range taskIDs {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i] < normalized[j] })
	return normalized
}

func (s *ChatService) unavailableScopeError(userID int64, ids []int64) error {
	tasks, err := s.repos.Task.ListByIDsForUser(userID, ids)
	if err != nil {
		return err
	}
	visible := map[int64]model.VideoTask{}
	for _, task := range tasks {
		visible[task.ID] = task
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		title := "已删除或无权访问的资料"
		if task, ok := visible[id]; ok {
			title = strings.TrimSpace(task.Title)
			if title == "" {
				title = task.Filename
			}
			if title == "" {
				title = "未命名资料"
			}
			title = "「" + title + "」"
		}
		parts = append(parts, title)
	}
	return fmt.Errorf("集合成员不可检索：%s。请完成索引或移出后再问", strings.Join(parts, "、"))
}
