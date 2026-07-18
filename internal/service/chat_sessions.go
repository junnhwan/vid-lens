package service

import (
	"fmt"

	"vid-lens/internal/model"
)

// 聊天会话的创建、标题、查询和删除。
func (s *ChatService) CreateSession(userID, taskID int64, title string) (*model.ChatSession, error) {
	task, err := s.repos.Task.FindByID(taskID)
	if err != nil {
		return nil, fmt.Errorf("任务不存在")
	}
	if task.UserID != userID {
		return nil, fmt.Errorf("无权访问此任务")
	}
	// 默认优先 LLM 视频标题，其次文件名；空标题禁止入库。
	title = ResolveChatSessionTitle(title, task.Title, task.Filename)
	session := &model.ChatSession{UserID: userID, TaskID: taskID, Title: title}
	if err := s.repos.Chat.CreateSession(session); err != nil {
		return nil, err
	}
	return session, nil
}

// UpdateSessionTitle 手动/自动改标题（校验归属）。
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

// maybeAutoTitleSession：仅当标题仍是「视频名/默认」占位时，用首条用户提问提炼短标题。
// 失败静默，不阻塞问答。
func (s *ChatService) maybeAutoTitleSession(session *model.ChatSession, firstUserQuestion string) {
	if session == nil {
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
	return s.repos.Chat.ListSessions(userID, taskID)
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
