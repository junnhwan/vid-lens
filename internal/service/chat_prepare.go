package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

// 按模式准备 RAG 或视频上下文，并构造检索管线。
//
// Spec 04 (A段)：散落的 intent/scope → 检索参数硬编码统一由 ExecutionPolicy 表达。
// 流程：识别 intent（占位 = classifyIntentPlaceholder）→ 取 ExecutionPolicy →
// 按字段走检索/生成。video_assistant 模式的"检索失败→转写兜底"降级路径保留
// （用户已拍板：ExecutionPolicy 只表达参数，兜底降级不进 policy）。
func normalizeChatMode(mode ChatMode) ChatMode {
	switch ChatMode(strings.TrimSpace(strings.ToLower(string(mode)))) {
	case ChatModeStrictRAG:
		return ChatModeStrictRAG
	case ChatModeVideoAssistant:
		return ChatModeVideoAssistant
	default:
		return ChatModeVideoAssistant
	}
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
	if session.ScopeType == model.ChatScopeKnowledgeBase || mode == ChatModeStrictRAG {
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
	taskIDs, err := s.sessionRetrievalTaskIDs(userID, session, profile.EmbeddingModel)
	if err != nil {
		return nil, err
	}

	// Spec 04 (A段)：intent → ExecutionPolicy 统一参数，消掉散落 if。
	intent := classifyIntentPlaceholder(question, session, mode)
	policy := PolicyFor(intent, scopeOfSession(session))
	if !policy.Retrieve {
		// strict_rag / KB 路径要求检索；policy.Retrieve=false 在此分支理论上不发生
		// （占位分类器对 KB/strict 不产出 overview/small_talk 关检索）。防御性兜底。
		return nil, errNoRetrievedContext
	}

	// 散落判定 1（topK 默认值 + topK>10→10 上限）由 ExecutionPolicy.ClampTopK
	// 统一表达（spec 04 数字占位符 A段）。
	topK = policy.ClampTopK(topK)
	if topK <= 0 {
		topK = s.cfg.TopK
	}

	// 散落判定 2（recentLimit=0 when KB）由 policy.Scope==collection 统一表达。
	// KnowledgeBase membership can change between turns. Until recent messages
	// carry member-safe provenance, keep history display-only so a removed
	// video's answer cannot be fed back into retrieval or generation.
	recentLimit := s.cfg.RecentTurns * 2
	var recent []model.ChatMessage
	if policy.Scope == ScopeCollection {
		recentLimit = 0
	} else {
		recent, err = s.loadRecentMessages(ctx, userID, sessionID, recentLimit)
		if err != nil {
			return nil, err
		}
	}

	pipeline := s.newRetrievalPipeline(topK, chat, profile)
	// 散落判定 3（KB → 强制 EnableVector=true/EnableBM25=false）由
	// policy.Scope==collection 统一表达；rerank 开关由 policy.Rerank 映射。
	pipeline.applyPolicy(policy)

	retrieval, err := pipeline.Retrieve(ctx, RetrievalPipelineRequest{
		UserID:         userID,
		TaskIDs:        taskIDs,
		Question:       question,
		Recent:         recent,
		TopK:           topK,
		EmbeddingModel: profile.EmbeddingModel,
		Embedding:      embedding,
	})
	if err != nil {
		return nil, err
	}
	contexts, citations := buildCitationSet(question, retrieval.Citations)
	if len(citations) == 0 {
		return nil, errNoRetrievedContext
	}
	messages := buildRAGMessages(contexts, recent, question)
	return &preparedRAGChat{
		Session:     session,
		Question:    question,
		TopK:        topK,
		RecentLimit: recentLimit,
		Contexts:    contexts,
		Citations:   citations,
		Messages:    messages,
		Policy:      policy,
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

	// Spec 04 (A段)：散落判定 4（isVideoOverviewQuestion → 关检索走视频上下文）
	// 由 ExecutionPolicy.Retrieve=false 统一表达。
	intent := classifyIntentPlaceholder(question, session, mode)
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
	return s.prepareVideoContextChat(session, question, recent, recentLimit)
}

func (s *ChatService) prepareVideoContextChat(session *model.ChatSession, question string, recent []model.ChatMessage, recentLimit int) (*preparedRAGChat, error) {
	contextText, err := s.videoContextText(session.TaskID)
	if err != nil {
		return nil, err
	}
	messages := buildVideoAssistantMessages(contextText, recent, question)
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

func (s *ChatService) videoContextText(taskID int64) (string, error) {
	sections := make([]string, 0, 2)
	if s.repos.Summary != nil {
		summary, err := s.repos.Summary.FindByTaskID(taskID)
		if err != nil {
			return "", err
		}
		if summary != nil && strings.TrimSpace(summary.Content) != "" {
			sections = append(sections, "视频摘要：\n"+trimRunes(strings.TrimSpace(summary.Content), maxVideoContextRunes/2))
		}
	}
	if s.repos.Transcription != nil {
		transcription, err := s.repos.Transcription.FindByTaskID(taskID)
		if err != nil {
			return "", err
		}
		if transcription != nil && strings.TrimSpace(transcription.Content) != "" {
			sections = append(sections, "视频转写：\n"+trimRunes(strings.TrimSpace(transcription.Content), maxVideoContextRunes))
		}
	}
	if len(sections) == 0 {
		return "", fmt.Errorf("当前视频没有可用的摘要或转写上下文")
	}
	return strings.Join(sections, "\n\n"), nil
}

func (s *ChatService) newRetrievalPipeline(topK int, chat ai.ChatClient, profile ai.Profile) *RetrievalPipeline {
	cfg := s.cfg.Retrieval
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
		reranker: reranker, CandidateK: s.candidateK(topK), MinScore: s.cfg.MinScore, Config: cfg}
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

func (s *ChatService) sessionRetrievalTaskIDs(userID int64, session *model.ChatSession, embeddingModel string) ([]int64, error) {
	if session.ScopeType != model.ChatScopeKnowledgeBase {
		if session.TaskID <= 0 {
			return nil, fmt.Errorf("视频会话缺少 task_id")
		}
		return []int64{session.TaskID}, nil
	}
	kb, err := s.repos.KnowledgeBase.FindByIDForUser(userID, session.KnowledgeBaseID)
	if err != nil {
		return nil, err
	}
	if kb == nil {
		return nil, fmt.Errorf("知识库不存在或无权限")
	}
	ids, err := s.repos.KnowledgeBase.ListMembershipTaskIDsForUser(userID, session.KnowledgeBaseID)
	if err != nil {
		return nil, err
	}
	ids = normalizeTaskIDs(ids)
	if len(ids) == 0 {
		return nil, fmt.Errorf("知识库没有可检索视频")
	}
	tasks, err := s.repos.Task.ListByIDsForUser(userID, ids)
	if err != nil {
		return nil, err
	}
	visibleTasks := make(map[int64]struct{}, len(tasks))
	for _, task := range tasks {
		visibleTasks[task.ID] = struct{}{}
	}
	indexes, err := s.repos.RAGIndex.ListByTaskIDsAndModel(userID, ids, embeddingModel)
	if err != nil {
		return nil, err
	}
	indexedTasks := make(map[int64]struct{}, len(indexes))
	for _, index := range indexes {
		if index.Status == model.RAGIndexStatusIndexed {
			indexedTasks[index.TaskID] = struct{}{}
		}
	}
	unavailable := make([]int64, 0)
	for _, taskID := range ids {
		_, visible := visibleTasks[taskID]
		_, indexed := indexedTasks[taskID]
		if !visible || !indexed {
			unavailable = append(unavailable, taskID)
		}
	}
	if len(unavailable) > 0 {
		parts := make([]string, len(unavailable))
		for i, id := range unavailable {
			parts[i] = strconv.FormatInt(id, 10)
		}
		return nil, fmt.Errorf("知识库成员不可用，task_ids=[%s]", strings.Join(parts, ","))
	}
	return ids, nil
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
