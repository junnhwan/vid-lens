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
	if session.ScopeType == model.ChatScopeKnowledgeBase || mode == ChatModeStrictRAG {
		return s.prepareRAGChat(ctx, userID, sessionID, question, topK, embedding, chat, profile)
	}
	return s.prepareVideoAssistantChat(ctx, userID, sessionID, question, topK, embedding, chat, profile)
}

func (s *ChatService) prepareRAGChat(ctx context.Context, userID, sessionID int64, question string, topK int, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile) (*preparedRAGChat, error) {
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
	if topK <= 0 {
		topK = s.cfg.TopK
	}
	if topK > 10 {
		topK = 10
	}

	recentLimit := s.cfg.RecentTurns * 2
	var recent []model.ChatMessage
	if session.ScopeType == model.ChatScopeKnowledgeBase {
		// Knowledge-base membership can change between turns. Until recent
		// messages carry member-safe provenance, keep history display-only so a
		// removed video's answer cannot be fed back into retrieval or generation.
		recentLimit = 0
	} else {
		recent, err = s.loadRecentMessages(ctx, userID, sessionID, recentLimit)
		if err != nil {
			return nil, err
		}
	}
	pipeline := s.newRetrievalPipeline(topK, chat, profile)
	if session.ScopeType == model.ChatScopeKnowledgeBase {
		cfg := DefaultRAGRetrievalConfig()
		if pipeline.Config != nil {
			cfg = *pipeline.Config
		}
		cfg.EnableVector = true
		cfg.EnableBM25 = false
		pipeline.Config = &cfg
	}
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
	}, nil
}

func (s *ChatService) prepareVideoAssistantChat(ctx context.Context, userID, sessionID int64, question string, topK int, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile) (*preparedRAGChat, error) {
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
	if isVideoOverviewQuestion(question) {
		return s.prepareVideoContextChat(session, question, recent, recentLimit)
	}

	prepared, ragErr := s.prepareRAGChat(ctx, userID, sessionID, question, topK, embedding, chat, profile)
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
