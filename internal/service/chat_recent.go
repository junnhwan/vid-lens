package service

import (
	"context"

	"vid-lens/internal/model"
)

// Redis 最近消息缓存与数据库回源。
func (s *ChatService) loadRecentMessages(ctx context.Context, userID, sessionID int64, limit int) ([]model.ChatMessage, error) {
	if s.memory != nil {
		cached, err := s.memory.GetRecentMessages(ctx, sessionID, limit)
		if err != nil {
			return nil, err
		}
		if len(cached) > 0 {
			return cached, nil
		}
	}

	recent, err := s.repos.Chat.ListRecentMessages(userID, sessionID, limit)
	if err != nil {
		return nil, err
	}
	if s.memory != nil && len(recent) > 0 {
		_ = s.memory.SaveRecentMessages(ctx, sessionID, recent, limit)
	}
	return recent, nil
}

func (s *ChatService) refreshRecentMemory(ctx context.Context, userID, sessionID int64, limit int) error {
	if s.memory == nil {
		return nil
	}
	recent, err := s.repos.Chat.ListRecentMessages(userID, sessionID, limit)
	if err != nil {
		return err
	}
	return s.memory.SaveRecentMessages(ctx, sessionID, recent, limit)
}
