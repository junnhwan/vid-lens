package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"vid-lens/internal/model"
)

const chatMemoryTTL = 7 * 24 * time.Hour

type RedisChatMemoryStore struct {
	redis redis.Cmdable
}

type chatMemoryMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func NewRedisChatMemoryStore(rdb redis.Cmdable) *RedisChatMemoryStore {
	return &RedisChatMemoryStore{redis: rdb}
}

func (s *RedisChatMemoryStore) GetRecentMessages(ctx context.Context, sessionID int64, limit int) ([]model.ChatMessage, error) {
	if s == nil || s.redis == nil || limit <= 0 {
		return nil, nil
	}
	raw, err := s.redis.Get(ctx, chatMemoryKey(sessionID)).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var cached []chatMemoryMessage
	if err := json.Unmarshal([]byte(raw), &cached); err != nil {
		return nil, err
	}
	if len(cached) > limit {
		cached = cached[len(cached)-limit:]
	}

	messages := make([]model.ChatMessage, 0, len(cached))
	for _, item := range cached {
		if item.Role != "user" && item.Role != "assistant" {
			continue
		}
		messages = append(messages, model.ChatMessage{Role: item.Role, Content: item.Content})
	}
	return messages, nil
}

func (s *RedisChatMemoryStore) SaveRecentMessages(ctx context.Context, sessionID int64, messages []model.ChatMessage, limit int) error {
	if s == nil || s.redis == nil || limit <= 0 {
		return nil
	}
	if len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}

	cached := make([]chatMemoryMessage, 0, len(messages))
	for _, msg := range messages {
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		cached = append(cached, chatMemoryMessage{Role: msg.Role, Content: msg.Content})
	}
	data, err := json.Marshal(cached)
	if err != nil {
		return err
	}
	return s.redis.Set(ctx, chatMemoryKey(sessionID), data, chatMemoryTTL).Err()
}

func chatMemoryKey(sessionID int64) string {
	return fmt.Sprintf("vidlens:chat:session:%d:recent", sessionID)
}
