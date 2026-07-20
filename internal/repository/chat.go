package repository

import (
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

// UpdateSessionTitle 仅更新标题与 updated_at（GORM 会自动维护 UpdatedAt）。
func (r *ChatRepository) UpdateSessionTitle(sessionID int64, title string) error {
	return r.db.Model(&model.ChatSession{}).
		Where("id = ?", sessionID).
		Update("title", title).Error
}

func (r *ChatRepository) ListSessions(userID int64, taskID int64) ([]model.ChatSession, error) {
	return r.ListSessionsFiltered(userID, taskID, 0, "")
}

func (r *ChatRepository) ListSessionsFiltered(userID, taskID, knowledgeBaseID int64, scopeType string) ([]model.ChatSession, error) {
	var sessions []model.ChatSession
	query := r.db.Where("user_id = ?", userID)
	if taskID > 0 {
		query = query.Where("task_id = ?", taskID)
	}
	if knowledgeBaseID > 0 {
		query = query.Where("knowledge_base_id = ?", knowledgeBaseID)
	}
	if scopeType != "" {
		query = query.Where("scope_type = ?", scopeType)
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

// CreateExchange atomically persists both chat messages and normalized assistant sources.
func (r *ChatRepository) CreateExchange(userID int64, userMessage, assistantMessage *model.ChatMessage, sourceTaskIDs []int64) error {
	if userMessage == nil || assistantMessage == nil {
		return gorm.ErrInvalidData
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(userMessage).Error; err != nil {
			return err
		}
		if err := tx.Create(assistantMessage).Error; err != nil {
			return err
		}
		sources := make([]model.ChatMessageSource, 0, len(sourceTaskIDs))
		for _, taskID := range sourceTaskIDs {
			source := model.ChatMessageSource{MessageID: assistantMessage.ID, SessionID: assistantMessage.SessionID, TaskID: taskID}
			if err := validateMessageSourceForUser(tx, userID, &source); err != nil {
				return err
			}
			sources = append(sources, source)
		}
		return createMessageSources(tx, sources)
	})
}

func (r *ChatRepository) CreateMessageSource(userID int64, source *model.ChatMessageSource) error {
	if source == nil {
		return gorm.ErrInvalidData
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := validateMessageSourceForUser(tx, userID, source); err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "message_id"}, {Name: "task_id"}},
			DoNothing: true,
		}).Create(source).Error
	})
}

func (r *ChatRepository) CreateMessageSources(userID int64, sources []model.ChatMessageSource) error {
	if len(sources) == 0 {
		return nil
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		for i := range sources {
			if err := validateMessageSourceForUser(tx, userID, &sources[i]); err != nil {
				return err
			}
		}
		return createMessageSources(tx, sources)
	})
}

func validateMessageSourceForUser(db *gorm.DB, userID int64, source *model.ChatMessageSource) error {
	var count int64
	err := db.Table("chat_messages AS cm").
		Joins("JOIN chat_sessions AS cs ON cs.id = cm.session_id").
		Joins("JOIN video_tasks AS vt ON vt.id = ? AND vt.deleted_at IS NULL", source.TaskID).
		Where(
			"cm.id = ? AND cm.user_id = ? AND cm.session_id = ? AND cs.id = ? AND cs.user_id = ? AND vt.user_id = ? AND "+
				"((cs.scope_type = ? AND cs.task_id = ?) OR (cs.scope_type = ? AND EXISTS ("+
				"SELECT 1 FROM knowledge_base_videos AS kbv "+
				"JOIN knowledge_bases AS kb ON kb.id = kbv.knowledge_base_id "+
				"WHERE kbv.knowledge_base_id = cs.knowledge_base_id AND kbv.task_id = ? AND kb.user_id = ?"+
				")))",
			source.MessageID, userID, source.SessionID, source.SessionID, userID, userID,
			model.ChatScopeVideo, source.TaskID, model.ChatScopeKnowledgeBase, source.TaskID, userID,
		).
		Count(&count).Error
	if err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func createMessageSources(db *gorm.DB, sources []model.ChatMessageSource) error {
	if len(sources) == 0 {
		return nil
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "message_id"}, {Name: "task_id"}},
		DoNothing: true,
	}).Create(&sources).Error
}

func (r *ChatRepository) ListSourceTaskIDsByMessageID(userID, messageID int64) ([]int64, error) {
	var taskIDs []int64
	err := r.db.Table("chat_message_sources AS cms").
		Joins("JOIN chat_messages AS cm ON cm.id = cms.message_id").
		Where("cm.user_id = ? AND cms.message_id = ?", userID, messageID).
		Distinct("cms.task_id").
		Order("cms.task_id ASC").
		Pluck("cms.task_id", &taskIDs).Error
	return taskIDs, err
}

func (r *ChatRepository) ListSourceTaskIDsBySessionID(userID, sessionID int64) ([]int64, error) {
	var taskIDs []int64
	err := r.db.Table("chat_message_sources AS cms").
		Joins("JOIN chat_sessions AS cs ON cs.id = cms.session_id").
		Where("cs.user_id = ? AND cms.session_id = ?", userID, sessionID).
		Distinct("cms.task_id").
		Order("cms.task_id ASC").
		Pluck("cms.task_id", &taskIDs).Error
	return taskIDs, err
}

func (r *ChatRepository) DeleteMessageSourcesByTaskID(taskID int64) error {
	return r.db.Where("task_id = ?", taskID).Delete(&model.ChatMessageSource{}).Error
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

// DeleteByTaskID removes direct video sessions and knowledge-base sessions whose
// persisted source rows reference the task. Callers can compose this with
// KnowledgeBaseRepository.DeleteMembershipsByTaskID in one repository transaction.
func (r *ChatRepository) DeleteByTaskID(taskID int64) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var sessionIDs []int64
		if err := tx.Model(&model.ChatSession{}).
			Distinct("chat_sessions.id").
			Joins("LEFT JOIN chat_message_sources AS cms ON cms.session_id = chat_sessions.id").
			Where("chat_sessions.task_id = ? OR (chat_sessions.scope_type = ? AND cms.task_id = ?)", taskID, model.ChatScopeKnowledgeBase, taskID).
			Pluck("chat_sessions.id", &sessionIDs).Error; err != nil {
			return err
		}
		if err := deleteChatSessions(tx, sessionIDs); err != nil {
			return err
		}
		return tx.Where("task_id = ?", taskID).Delete(&model.ChatMessageSource{}).Error
	})
}

// DeleteSession 删除单个会话及其消息（调用方应已校验归属于该用户）
func (r *ChatRepository) DeleteSession(sessionID int64) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		return deleteChatSessions(tx, []int64{sessionID})
	})
}

func deleteChatSessions(db *gorm.DB, sessionIDs []int64) error {
	if len(sessionIDs) == 0 {
		return nil
	}
	if err := db.Where("session_id IN ?", sessionIDs).Delete(&model.ChatMessageSource{}).Error; err != nil {
		return err
	}
	if err := db.Where("session_id IN ?", sessionIDs).Delete(&model.ChatMessage{}).Error; err != nil {
		return err
	}
	return db.Where("id IN ?", sessionIDs).Delete(&model.ChatSession{}).Error
}
