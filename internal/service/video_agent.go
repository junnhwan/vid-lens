package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

type VideoAgentTemplate string

const (
	VideoAgentDirectQA       VideoAgentTemplate = "direct_qa"
	VideoAgentSummarizeTopic VideoAgentTemplate = "summarize_topic"
	VideoAgentCompareTopics  VideoAgentTemplate = "compare_topics"
	VideoAgentCritiqueTopic  VideoAgentTemplate = "critique_topic"
)

type VideoAgentRequest struct {
	UserID    int64
	SessionID int64
	Question  string
	TopK      int
}

type VideoAgentResult struct {
	MessageID int64            `json:"message_id"`
	Answer    string           `json:"answer"`
	Template  string           `json:"template"`
	Citations []RetrievedChunk `json:"citations"`
	Trace     []VideoAgentStep `json:"trace"`
	Model     string           `json:"model"`
}

type VideoAgentStep struct {
	Name      string         `json:"name"`
	Tool      string         `json:"tool"`
	Input     map[string]any `json:"input,omitempty"`
	OutputRef string         `json:"output_ref,omitempty"`
	Error     string         `json:"error,omitempty"`
}

type VideoAgentTemplateRequest struct {
	UserID         int64
	TaskID         int64
	Question       string
	EmbeddingModel string
}

type VideoAgentService struct {
	chatSvc *ChatService
}

type VideoAgentExecutionError struct {
	Message string
	Trace   []VideoAgentStep
}

func (e *VideoAgentExecutionError) Error() string {
	return e.Message
}

func NewVideoAgentService(chatSvc *ChatService) *VideoAgentService {
	return &VideoAgentService{chatSvc: chatSvc}
}

func ClassifyVideoAgentTemplate(question string) VideoAgentTemplate {
	question = strings.TrimSpace(question)
	if containsAny(question, "对比", "区别", "前后", "变化") {
		return VideoAgentCompareTopics
	}
	if containsAny(question, "总结", "归纳", "概括") {
		return VideoAgentSummarizeTopic
	}
	if containsAny(question, "问题", "风险", "不足", "不严谨", "反驳") {
		return VideoAgentCritiqueTopic
	}
	return VideoAgentDirectQA
}

func (s *VideoAgentService) Ask(ctx context.Context, req VideoAgentRequest, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile) (*VideoAgentResult, error) {
	req.Question = strings.TrimSpace(req.Question)
	if req.Question == "" {
		return nil, fmt.Errorf("问题不能为空")
	}
	if s == nil || s.chatSvc == nil {
		return nil, fmt.Errorf("agent chat service 不能为空")
	}
	if s.chatSvc.retriever == nil {
		return nil, fmt.Errorf("当前视频尚未构建 RAG 索引")
	}
	session, err := s.chatSvc.repos.Chat.FindSessionForUser(req.UserID, req.SessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, fmt.Errorf("无权访问此会话")
	}
	if req.TopK <= 0 {
		req.TopK = s.chatSvc.cfg.TopK
	}
	if req.TopK > 10 {
		req.TopK = 10
	}

	recentLimit := s.chatSvc.cfg.RecentTurns * 2
	recent, err := s.chatSvc.loadRecentMessages(ctx, req.UserID, req.SessionID, recentLimit)
	if err != nil {
		return nil, err
	}
	embedding, chat = s.chatSvc.observedAIClients(req.UserID, req.SessionID, session.TaskID, embedding, chat, profile)
	template := ClassifyVideoAgentTemplate(req.Question)
	tools := NewVideoAgentTools(s.chatSvc.repos, s.chatSvc.newRetrievalPipeline(req.TopK, chat), chat)
	trace := make([]VideoAgentStep, 0, 4)

	search, step, err := tools.SearchTranscript(ctx, SearchTranscriptInput{
		UserID:         req.UserID,
		TaskID:         session.TaskID,
		Question:       req.Question,
		Recent:         recent,
		TopK:           req.TopK,
		EmbeddingModel: profile.EmbeddingModel,
		Embedding:      embedding,
	})
	trace = append(trace, step)
	if err != nil {
		return nil, newVideoAgentExecutionError(err, trace)
	}
	if len(search.Citations) == 0 {
		return nil, newVideoAgentExecutionError(fmt.Errorf("未检索到足够相关的视频片段"), trace)
	}

	answer, citations, trace, err := s.executeTemplate(ctx, tools, template, req, profile.EmbeddingModel, session.TaskID, search.Citations, trace)
	if err != nil {
		return nil, newVideoAgentExecutionError(err, trace)
	}
	result := &VideoAgentResult{
		Answer:    answer,
		Template:  string(template),
		Citations: citations,
		Trace:     trace,
		Model:     profile.LLMModel,
	}
	if err := s.saveAgentExchange(ctx, req.UserID, req.SessionID, req.Question, result, recentLimit); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *VideoAgentService) executeTemplate(ctx context.Context, tools *VideoAgentTools, template VideoAgentTemplate, req VideoAgentRequest, embeddingModel string, taskID int64, citations []RetrievedChunk, trace []VideoAgentStep) (string, []RetrievedChunk, []VideoAgentStep, error) {
	return ExecuteVideoAgentTemplate(ctx, tools, template, VideoAgentTemplateRequest{
		UserID:         req.UserID,
		TaskID:         taskID,
		Question:       req.Question,
		EmbeddingModel: embeddingModel,
	}, citations, trace)
}

func ExecuteVideoAgentTemplate(ctx context.Context, tools *VideoAgentTools, template VideoAgentTemplate, req VideoAgentTemplateRequest, citations []RetrievedChunk, trace []VideoAgentStep) (string, []RetrievedChunk, []VideoAgentStep, error) {
	switch template {
	case VideoAgentSummarizeTopic:
		segments, windowTrace, err := loadAgentWindowSegments(ctx, tools, req.UserID, req.TaskID, req.EmbeddingModel, citations, len(citations))
		trace = append(trace, windowTrace...)
		if err != nil {
			return "", nil, trace, err
		}
		summary, step, err := tools.SummarizeSegments(ctx, SummarizeSegmentsInput{Question: req.Question, Segments: segments})
		trace = append(trace, step)
		if err != nil {
			return "", nil, trace, err
		}
		final, step, err := tools.BuildCitedAnswer(ctx, BuildCitedAnswerInput{Question: req.Question, Intermediate: summary.Summary, Citations: citations})
		trace = append(trace, step)
		if err != nil {
			return "", nil, trace, err
		}
		return final.Answer, final.Citations, trace, nil
	case VideoAgentCompareTopics:
		groups, windowTrace, err := loadAgentWindowGroups(ctx, tools, req.UserID, req.TaskID, req.EmbeddingModel, citations, 2)
		trace = append(trace, windowTrace...)
		if err != nil {
			return "", nil, trace, err
		}
		comparison, step, err := tools.CompareSegments(ctx, CompareSegmentsInput{Question: req.Question, Groups: groups})
		trace = append(trace, step)
		if err != nil {
			return "", nil, trace, err
		}
		final, step, err := tools.BuildCitedAnswer(ctx, BuildCitedAnswerInput{Question: req.Question, Intermediate: comparison.Comparison, Citations: citations})
		trace = append(trace, step)
		if err != nil {
			return "", nil, trace, err
		}
		return final.Answer, final.Citations, trace, nil
	case VideoAgentCritiqueTopic:
		segments, windowTrace, err := loadAgentWindowSegments(ctx, tools, req.UserID, req.TaskID, req.EmbeddingModel, citations, len(citations))
		trace = append(trace, windowTrace...)
		if err != nil {
			return "", nil, trace, err
		}
		summary, step, err := tools.SummarizeSegments(ctx, SummarizeSegmentsInput{
			Question: "围绕用户问题总结这些片段中的问题、风险、不足或不严谨之处：" + req.Question,
			Segments: segments,
		})
		trace = append(trace, step)
		if err != nil {
			return "", nil, trace, err
		}
		final, step, err := tools.BuildCitedAnswer(ctx, BuildCitedAnswerInput{Question: req.Question, Intermediate: summary.Summary, Citations: citations})
		trace = append(trace, step)
		if err != nil {
			return "", nil, trace, err
		}
		return final.Answer, final.Citations, trace, nil
	default:
		final, step, err := tools.BuildCitedAnswer(ctx, BuildCitedAnswerInput{
			Question:     req.Question,
			Intermediate: "请直接基于检索到的视频转写片段回答用户问题。",
			Citations:    citations,
		})
		trace = append(trace, step)
		if err != nil {
			return "", nil, trace, err
		}
		return final.Answer, final.Citations, trace, nil
	}
}

func loadAgentWindowSegments(ctx context.Context, tools *VideoAgentTools, userID, taskID int64, embeddingModel string, citations []RetrievedChunk, maxWindows int) ([]TranscriptSegment, []VideoAgentStep, error) {
	groups, trace, err := loadAgentWindowGroups(ctx, tools, userID, taskID, embeddingModel, citations, maxWindows)
	if err != nil {
		return nil, trace, err
	}
	segments := make([]TranscriptSegment, 0)
	for _, group := range groups {
		segments = append(segments, group.Segments...)
	}
	return segments, trace, nil
}

func loadAgentWindowGroups(ctx context.Context, tools *VideoAgentTools, userID, taskID int64, embeddingModel string, citations []RetrievedChunk, maxWindows int) ([]TranscriptSegmentGroup, []VideoAgentStep, error) {
	if maxWindows <= 0 || maxWindows > len(citations) {
		maxWindows = len(citations)
	}
	groups := make([]TranscriptSegmentGroup, 0, maxWindows)
	trace := make([]VideoAgentStep, 0, maxWindows)
	for i := 0; i < maxWindows; i++ {
		citation := citations[i]
		window, step, err := tools.GetTranscriptWindow(ctx, TranscriptWindowInput{
			UserID:         userID,
			TaskID:         taskID,
			EmbeddingModel: embeddingModel,
			ChunkIndex:     citation.ChunkIndex,
			Radius:         1,
		})
		trace = append(trace, step)
		if err != nil {
			return nil, trace, err
		}
		groups = append(groups, TranscriptSegmentGroup{
			Label:    fmt.Sprintf("chunk_%d_window_%d_%d", citation.ChunkIndex, window.StartIndex, window.EndIndex),
			Segments: window.Segments,
		})
	}
	return groups, trace, nil
}

func (s *VideoAgentService) saveAgentExchange(ctx context.Context, userID, sessionID int64, question string, result *VideoAgentResult, recentLimit int) error {
	if err := s.chatSvc.repos.Chat.CreateMessage(&model.ChatMessage{
		SessionID: sessionID,
		UserID:    userID,
		Role:      "user",
		Content:   question,
	}); err != nil {
		return err
	}
	snapshot, err := json.Marshal(struct {
		Template  string           `json:"template"`
		Citations []RetrievedChunk `json:"citations"`
		Trace     []VideoAgentStep `json:"trace"`
	}{
		Template:  result.Template,
		Citations: result.Citations,
		Trace:     result.Trace,
	})
	if err != nil {
		return err
	}
	snapshotText := string(snapshot)
	assistantMessage := &model.ChatMessage{
		SessionID:         sessionID,
		UserID:            userID,
		Role:              "assistant",
		Content:           result.Answer,
		RetrievalSnapshot: &snapshotText,
		ModelName:         result.Model,
	}
	if err := s.chatSvc.repos.Chat.CreateMessage(assistantMessage); err != nil {
		return err
	}
	_ = s.chatSvc.refreshRecentMemory(ctx, userID, sessionID, recentLimit)
	result.MessageID = assistantMessage.ID
	return nil
}

func newVideoAgentExecutionError(err error, trace []VideoAgentStep) error {
	return &VideoAgentExecutionError{
		Message: err.Error(),
		Trace:   append([]VideoAgentStep(nil), trace...),
	}
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
