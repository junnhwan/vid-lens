package service

import (
	"fmt"
	"strings"

	"vid-lens/internal/model"
)

type CreateChatSessionRequest struct {
	TaskID          int64
	ScopeType       string
	KnowledgeBaseID int64
	Title           string
}

type ListChatSessionsFilter struct {
	TaskID          int64
	KnowledgeBaseID int64
	ScopeType       string
}

// CreateSession keeps the original video-session API compatible.
func (s *ChatService) CreateSession(userID, taskID int64, title string) (*model.ChatSession, error) {
	return s.CreateScopedSession(userID, CreateChatSessionRequest{TaskID: taskID, Title: title})
}

func (s *ChatService) CreateScopedSession(userID int64, req CreateChatSessionRequest) (*model.ChatSession, error) {
	scopeType := strings.TrimSpace(strings.ToLower(req.ScopeType))
	if scopeType == "" && req.TaskID > 0 && req.KnowledgeBaseID == 0 {
		scopeType = model.ChatScopeVideo
	}

	session := &model.ChatSession{UserID: userID, ScopeType: scopeType}
	switch scopeType {
	case model.ChatScopeVideo:
		if req.TaskID <= 0 || req.KnowledgeBaseID != 0 {
			return nil, fmt.Errorf("video 会话必须且只能指定 task_id")
		}
		task, err := s.repos.Task.FindByID(req.TaskID)
		if err != nil {
			return nil, fmt.Errorf("任务不存在")
		}
		if task.UserID != userID {
			return nil, fmt.Errorf("无权访问此任务")
		}
		session.TaskID = task.ID
		session.Title = ResolveChatSessionTitle(req.Title, task.Title, task.Filename)
	case model.ChatScopeKnowledgeBase:
		if req.KnowledgeBaseID <= 0 || req.TaskID != 0 {
			return nil, fmt.Errorf("knowledge_base 会话必须且只能指定 knowledge_base_id")
		}
		kb, err := s.repos.KnowledgeBase.FindByIDForUser(userID, req.KnowledgeBaseID)
		if err != nil {
			return nil, err
		}
		if kb == nil {
			return nil, fmt.Errorf("知识库不存在或无权限")
		}
		session.KnowledgeBaseID = kb.ID
		session.Title = ResolveChatSessionTitle(req.Title, kb.Name, "")
	default:
		return nil, fmt.Errorf("scope_type 必须为 video 或 knowledge_base")
	}
	if err := s.repos.Chat.CreateSession(session); err != nil {
		return nil, err
	}
	return session, nil
}

func (s *ChatService) UpdateSessionTitle(userID, sessionID int64, title string) error {
	session, err := s.repos.Chat.FindSessionForUser(userID, sessionID)
	if err != nil {
		return err
	}
	if session == nil {
		return fmt.Errorf("会话不存在或无权限")
	}
	title = sanitizeChatSessionTitle(title)
	if title == "" {
		return fmt.Errorf("标题不能为空")
	}
	return s.repos.Chat.UpdateSessionTitle(sessionID, title)
}

func (s *ChatService) maybeAutoTitleSession(session *model.ChatSession, firstUserQuestion string) {
	if session == nil || session.ScopeType == model.ChatScopeKnowledgeBase || session.TaskID <= 0 {
		return
	}
	task, err := s.repos.Task.FindByID(session.TaskID)
	if err != nil || task == nil {
		return
	}
	next, ok := AutoTitleChatSessionFromQuestion(session.Title, task.Title, task.Filename, firstUserQuestion)
	if !ok {
		return
	}
	if err := s.repos.Chat.UpdateSessionTitle(session.ID, next); err != nil {
		return
	}
	session.Title = next
}

func (s *ChatService) ListSessions(userID, taskID int64) ([]model.ChatSession, error) {
	return s.ListSessionsWithFilter(userID, ListChatSessionsFilter{TaskID: taskID})
}

func (s *ChatService) ListSessionsWithFilter(userID int64, filter ListChatSessionsFilter) ([]model.ChatSession, error) {
	scopeType := strings.TrimSpace(strings.ToLower(filter.ScopeType))
	if scopeType != "" && scopeType != model.ChatScopeVideo && scopeType != model.ChatScopeKnowledgeBase {
		return nil, fmt.Errorf("scope_type 必须为 video 或 knowledge_base")
	}
	return s.repos.Chat.ListSessionsFiltered(userID, filter.TaskID, filter.KnowledgeBaseID, scopeType)
}

func (s *ChatService) ListMessages(userID, sessionID int64) ([]model.ChatMessage, error) {
	session, err := s.repos.Chat.FindSessionForUser(userID, sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, fmt.Errorf("无权访问此会话")
	}
	return s.repos.Chat.ListMessages(userID, sessionID)
}

func (s *ChatService) DeleteSession(userID, sessionID int64) error {
	session, err := s.repos.Chat.FindSessionForUser(userID, sessionID)
	if err != nil {
		return err
	}
	if session == nil {
		return fmt.Errorf("会话不存在或无权限")
	}
	return s.repos.Chat.DeleteSession(sessionID)
}
