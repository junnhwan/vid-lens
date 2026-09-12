package service

// Video agent is an experimental tool-loop QA path.
// Product default is stream/sync ChatService; keep agent changes isolated.

import (
	"context"
	"errors"
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
	UserID       int64
	SessionID    int64
	Question     string
	TopK         int
	MemoryPolicy *model.EffectiveMemoryPolicy
}

type VideoAgentResult struct {
	Degraded     bool                        `json:"degraded,omitempty"`
	MessageID    int64                       `json:"message_id"`
	Answer       string                      `json:"answer"`
	Template     string                      `json:"template"`
	Citations    []Citation                  `json:"citations"`
	Trace        []VideoAgentStep            `json:"trace"`
	Model        string                      `json:"model"`
	RunID        string                      `json:"run_id,omitempty"`
	Mode         string                      `json:"mode,omitempty"`
	Memory       *MemorySnapshotIdentity     `json:"memory,omitempty"`
	MemoryPolicy model.EffectiveMemoryPolicy `json:"memory_policy"`
}

type VideoAgentStep struct {
	Name      string         `json:"name"`
	Tool      string         `json:"tool"`
	Input     map[string]any `json:"input,omitempty"`
	OutputRef string         `json:"output_ref,omitempty"`
	Error     string         `json:"error,omitempty"`
}

type VideoAgentService struct {
	chatSvc            *ChatService
	executionJournal   *AgentExecutionJournal
	visualInvestigator VisualInvestigator
}

type VideoAgentExecutionError struct {
	Message string
	Trace   []VideoAgentStep
	Cause   error
}

func (e *VideoAgentExecutionError) Error() string {
	return e.Message
}

func (e *VideoAgentExecutionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func NewVideoAgentService(chatSvc *ChatService) *VideoAgentService {
	service := &VideoAgentService{chatSvc: chatSvc}
	if chatSvc != nil && chatSvc.repos != nil && chatSvc.repos.AgentExecution != nil {
		service.executionJournal = NewAgentExecutionJournal(chatSvc.repos.AgentExecution)
	}
	return service
}

func (s *VideoAgentService) SetVisualInvestigator(investigator VisualInvestigator) {
	if s != nil {
		s.visualInvestigator = investigator
	}
}

func (s *VideoAgentService) Ask(ctx context.Context, req VideoAgentRequest, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile) (*VideoAgentResult, error) {
	return s.RunAgent(ctx, VideoAgentLoopRequest{UserID: req.UserID, SessionID: req.SessionID, Goal: req.Question, TopK: req.TopK}, embedding, chat, profile)
}

func (s *VideoAgentService) findVideoAgentSession(userID, sessionID int64) (*model.ChatSession, error) {
	if s == nil || s.chatSvc == nil {
		return nil, fmt.Errorf("agent chat service 不能为空")
	}
	if s.chatSvc.repos == nil || s.chatSvc.repos.Chat == nil {
		return nil, fmt.Errorf("agent chat repository 不能为空")
	}
	session, err := s.chatSvc.repos.Chat.FindSessionForUser(userID, sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, fmt.Errorf("无权访问此会话")
	}
	if session.ScopeType == model.ChatScopeKnowledgeBase {
		return nil, fmt.Errorf("知识库会话暂不支持 Agent 问答")
	}
	if session.ScopeType != "" && session.ScopeType != model.ChatScopeVideo {
		return nil, fmt.Errorf("Agent 仅支持单视频会话")
	}
	return session, nil
}

// saveAgentRunExchange persists a terminal Research exchange under the run's
// idempotency key. A retry gets the original assistant message ID and does
// not append a second user/assistant pair.
func (s *VideoAgentService) saveAgentRunExchange(ctx context.Context, userID, sessionID int64, question string, result *VideoAgentResult, recentLimit int) error {
	if s == nil || s.chatSvc == nil || s.chatSvc.repos == nil || s.chatSvc.repos.Chat == nil || result == nil || strings.TrimSpace(result.RunID) == "" {
		return errors.New("agent run exchange parameters are invalid")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	snapshot, err := MarshalAgentSnapshot(result)
	if err != nil {
		return err
	}
	snapshotText := string(snapshot)
	userMessage := &model.ChatMessage{SessionID: sessionID, UserID: userID, Role: "user", Content: question}
	assistantMessage := &model.ChatMessage{SessionID: sessionID, UserID: userID, Role: "assistant", Content: result.Answer, RetrievalSnapshot: &snapshotText, ModelName: result.Model}
	created, userMessageID, assistantMessageID, err := s.chatSvc.repos.Chat.CreateAgentRunExchange(userID, result.RunID, userMessage, assistantMessage, nil)
	if err != nil {
		return err
	}
	result.MessageID = assistantMessageID
	if !created {
		stored, err := loadAgentRunResult(ctx, s, userID, sessionID, result.RunID)
		if err != nil {
			return err
		}
		if stored == nil {
			return errors.New("persisted agent answer unavailable")
		}
		*result = *stored
		return nil
	}
	if session, findErr := s.chatSvc.repos.Chat.FindSessionForUser(userID, sessionID); findErr == nil && session != nil {
		s.chatSvc.maybeAutoTitleSession(session, question)
	}
	_ = s.chatSvc.refreshRecentMemory(ctx, userID, sessionID, recentLimit)
	if result.MemoryPolicy.EffectiveEnabled && s.chatSvc.memoryCapture != nil {
		_ = s.chatSvc.memoryCapture.EnqueueExtraction(MemoryExtractionRequest{
			UserID: userID, SessionID: sessionID, UserText: question, SourceRef: fmt.Sprintf("chat_message:%d", userMessageID),
		})
	}
	return nil
}

func (s *VideoAgentService) loadAgentMemorySnapshot(ctx context.Context, userID, taskID int64, runID, query string, policy model.EffectiveMemoryPolicy) *MemorySnapshot {
	if s == nil || s.chatSvc == nil || s.chatSvc.longTermMemory == nil || !policy.EffectiveEnabled {
		return nil
	}
	snapshot, err := s.chatSvc.longTermMemory.Snapshot(ctx, MemorySnapshotRequest{
		UserID: userID,
		Query:  query,
		Scopes: []MemoryScope{
			{Type: model.MemoryScopeUser, ID: fmt.Sprintf("%d", userID)},
			{Type: model.MemoryScopeVideo, ID: fmt.Sprintf("%d", taskID)},
			{Type: model.MemoryScopeRun, ID: runID},
		},
	})
	if err != nil {
		return nil
	}
	return &snapshot
}

func newVideoAgentExecutionError(err error, trace []VideoAgentStep) error {
	return &VideoAgentExecutionError{
		Message: err.Error(),
		Trace:   append([]VideoAgentStep(nil), trace...),
		Cause:   err,
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
