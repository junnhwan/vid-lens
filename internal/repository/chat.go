package repository

import (
	"errors"

	"gorm.io/gorm"
	"vid-lens/internal/model"
)

type ChatRepository struct {
	db *gorm.DB
}

func NewChatRepository(db *gorm.DB) *ChatRepository {
	return &ChatRepository{db: db}
}

func (r *ChatRepository) CreateSession(session *model.ChatSession) error {
	return r.db.Create(session).Error
}

func (r *ChatRepository) ListSessions(userID int64, taskID int64) ([]model.ChatSession, error) {
	var sessions []model.ChatSession
	query := r.db.Where("user_id = ?", userID)
	if taskID > 0 {
		query = query.Where("task_id = ?", taskID)
	}
	err := query.Order("updated_at desc, id desc").Find(&sessions).Error
	return sessions, err
}

func (r *ChatRepository) FindSessionForUser(userID, sessionID int64) (*model.ChatSession, error) {
	var session model.ChatSession
	err := r.db.Where("user_id = ? AND id = ?", userID, sessionID).First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *ChatRepository) CreateMessage(message *model.ChatMessage) error {
	return r.db.Create(message).Error
}

func (r *ChatRepository) ListMessages(userID, sessionID int64) ([]model.ChatMessage, error) {
	var messages []model.ChatMessage
	err := r.db.Where("user_id = ? AND session_id = ?", userID, sessionID).
		Order("id asc").
		Find(&messages).Error
	return messages, err
}

func (r *ChatRepository) ListRecentMessages(userID, sessionID int64, limit int) ([]model.ChatMessage, error) {
	if limit <= 0 {
		return nil, nil
	}
	var desc []model.ChatMessage
	if err := r.db.Where("user_id = ? AND session_id = ?", userID, sessionID).
		Order("id desc").
		Limit(limit).
		Find(&desc).Error; err != nil {
		return nil, err
	}
	messages := make([]model.ChatMessage, len(desc))
	for i := range desc {
		messages[len(desc)-1-i] = desc[i]
	}
	return messages, nil
}
