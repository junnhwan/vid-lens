package repository

import (
	"encoding/json"
	"errors"
	"strings"

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

// CreateExchange atomically revalidates the current chat scope before it
// persists both messages and normalized assistant sources. Scope owners are
// locked in the same order used by deletion/member mutations so a concurrent
// lifecycle change either happens entirely before or entirely after the exchange.
func (r *ChatRepository) CreateExchange(userID int64, userMessage, assistantMessage *model.ChatMessage, sourceTaskIDs []int64) error {
	_, _, _, err := r.createExchange(userID, "", userMessage, assistantMessage, sourceTaskIDs)
	return err
}

// CreateAgentRunExchange creates at most one user/assistant exchange for a
// durable Agent run. The lookup and insert happen while the session row is
// locked, so concurrent requests for the same run observe the same messages.
func (r *ChatRepository) CreateAgentRunExchange(userID int64, runID string, userMessage, assistantMessage *model.ChatMessage, sourceTaskIDs []int64) (created bool, userMessageID, assistantMessageID int64, err error) {
	if strings.TrimSpace(runID) == "" {
		return false, 0, 0, gorm.ErrInvalidData
	}
	return r.createExchange(userID, strings.TrimSpace(runID), userMessage, assistantMessage, sourceTaskIDs)
}

func (r *ChatRepository) createExchange(userID int64, runID string, userMessage, assistantMessage *model.ChatMessage, sourceTaskIDs []int64) (created bool, userMessageID, assistantMessageID int64, err error) {
	if userMessage == nil || assistantMessage == nil ||
		userMessage.UserID != userID || assistantMessage.UserID != userID ||
		userMessage.SessionID <= 0 || userMessage.SessionID != assistantMessage.SessionID {
		return false, 0, 0, gorm.ErrInvalidData
	}
	sourceTaskIDs, err = normalizeSourceTaskIDs(sourceTaskIDs)
	if err != nil {
		return false, 0, 0, err
	}

	err = r.db.Transaction(func(tx *gorm.DB) error {
		// Read the owner-scoped session first only to discover the resource lock
		// order. The session is locked and compared again below before writes.
		var observed model.ChatSession
		if err := tx.Where("id = ? AND user_id = ?", userMessage.SessionID, userID).First(&observed).Error; err != nil {
			return err
		}

		switch observed.ScopeType {
		case model.ChatScopeVideo:
			var task model.VideoTask
			if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
				Where("id = ? AND user_id = ?", observed.TaskID, userID).
				First(&task).Error; err != nil {
				return err
			}
		case model.ChatScopeKnowledgeBase:
			var knowledgeBase model.KnowledgeBase
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ? AND user_id = ?", observed.KnowledgeBaseID, userID).
				First(&knowledgeBase).Error; err != nil {
				return err
			}
		default:
			return gorm.ErrInvalidData
		}

		var session model.ChatSession
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND user_id = ?", observed.ID, userID).
			First(&session).Error; err != nil {
			return err
		}
		if !sameChatSessionScope(&observed, &session) {
			return gorm.ErrRecordNotFound
		}
		if err := validateExchangeSources(tx, userID, &session, sourceTaskIDs); err != nil {
			return err
		}
		if runID != "" {
			var run model.AgentRun
			if err := tx.Where("id = ? AND user_id = ? AND session_id = ? AND scope_type = ? AND task_id = ? AND knowledge_base_id = ?", runID, userID, session.ID, session.ScopeType, session.TaskID, session.KnowledgeBaseID).First(&run).Error; err != nil {
				return err
			}
			existingAssistant, existingUser, err := findAgentRunExchange(tx, userID, session.ID, runID)
			if err != nil {
				return err
			}
			if existingAssistant != nil {
				created, assistantMessageID = false, existingAssistant.ID
				if existingUser != nil {
					userMessageID = existingUser.ID
				}
				return nil
			}
		}

		if err := tx.Create(userMessage).Error; err != nil {
			return err
		}
		if err := tx.Create(assistantMessage).Error; err != nil {
			return err
		}
		sources := make([]model.ChatMessageSource, 0, len(sourceTaskIDs))
		for _, taskID := range sourceTaskIDs {
			sources = append(sources, model.ChatMessageSource{
				MessageID: assistantMessage.ID,
				SessionID: assistantMessage.SessionID,
				TaskID:    taskID,
			})
		}
		if err := createMessageSources(tx, sources); err != nil {
			return err
		}
		created, userMessageID, assistantMessageID = true, userMessage.ID, assistantMessage.ID
		return nil
	})
	return created, userMessageID, assistantMessageID, err
}

func findAgentRunExchange(tx *gorm.DB, userID, sessionID int64, runID string) (*model.ChatMessage, *model.ChatMessage, error) {
	var assistants []model.ChatMessage
	if err := tx.Where("user_id = ? AND session_id = ? AND role = ? AND retrieval_snapshot IS NOT NULL", userID, sessionID, "assistant").Order("id DESC").Find(&assistants).Error; err != nil {
		return nil, nil, err
	}
	for index := range assistants {
		snapshot := assistants[index].RetrievalSnapshot
		if snapshot == nil {
			continue
		}
		var envelope struct {
			RunID string `json:"run_id"`
		}
		if json.Unmarshal([]byte(*snapshot), &envelope) != nil || envelope.RunID != runID {
			continue
		}
		var userMessage model.ChatMessage
		err := tx.Where("user_id = ? AND session_id = ? AND role = ? AND id < ?", userID, sessionID, "user", assistants[index].ID).Order("id DESC").First(&userMessage).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &assistants[index], nil, nil
		}
		if err != nil {
			return nil, nil, err
		}
		return &assistants[index], &userMessage, nil
	}
	return nil, nil, nil
}

func normalizeSourceTaskIDs(taskIDs []int64) ([]int64, error) {
	unique := make([]int64, 0, len(taskIDs))
	seen := make(map[int64]struct{}, len(taskIDs))
	for _, taskID := range taskIDs {
		if taskID <= 0 {
			return nil, gorm.ErrInvalidData
		}
		if _, ok := seen[taskID]; ok {
			continue
		}
		seen[taskID] = struct{}{}
		unique = append(unique, taskID)
	}
	return unique, nil
}

func sameChatSessionScope(left, right *model.ChatSession) bool {
	return left != nil && right != nil &&
		left.ID == right.ID && left.UserID == right.UserID &&
		left.ScopeType == right.ScopeType && left.TaskID == right.TaskID &&
		left.KnowledgeBaseID == right.KnowledgeBaseID
}

func validateExchangeSources(db *gorm.DB, userID int64, session *model.ChatSession, sourceTaskIDs []int64) error {
	switch session.ScopeType {
	case model.ChatScopeVideo:
		for _, taskID := range sourceTaskIDs {
			if taskID != session.TaskID {
				return gorm.ErrRecordNotFound
			}
		}
		return nil
	case model.ChatScopeKnowledgeBase:
		if len(sourceTaskIDs) == 0 {
			return nil
		}
		var count int64
		err := db.Table("knowledge_base_videos AS kbv").
			Joins("JOIN video_tasks AS vt ON vt.id = kbv.task_id AND vt.deleted_at IS NULL").
			Where("kbv.knowledge_base_id = ? AND kbv.task_id IN ? AND vt.user_id = ?", session.KnowledgeBaseID, sourceTaskIDs, userID).
			Distinct("kbv.task_id").
			Count(&count).Error
		if err != nil {
			return err
		}
		if count != int64(len(sourceTaskIDs)) {
			return gorm.ErrRecordNotFound
		}
		return nil
	default:
		return gorm.ErrInvalidData
	}
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

func (r *ChatRepository) UpdateAssistantSnapshot(userID, sessionID, messageID int64, snapshot string) (bool, error) {
	if userID <= 0 || sessionID <= 0 || messageID <= 0 || strings.TrimSpace(snapshot) == "" {
		return false, gorm.ErrInvalidData
	}
	result := r.db.Model(&model.ChatMessage{}).
		Where("id = ? AND user_id = ? AND session_id = ? AND role = ?", messageID, userID, sessionID, "assistant").
		Update("retrieval_snapshot", snapshot)
	return result.RowsAffected == 1, result.Error
}

// UpdateAssistantResult publishes a validated assistant answer and its
// compatibility snapshot together. This prevents a final answer from becoming
// visible with a stale, pre-validation snapshot (or vice versa).
func (r *ChatRepository) UpdateAssistantResult(userID, sessionID, messageID int64, content, snapshot, modelName string) (bool, error) {
	if userID <= 0 || sessionID <= 0 || messageID <= 0 || strings.TrimSpace(content) == "" || strings.TrimSpace(snapshot) == "" {
		return false, gorm.ErrInvalidData
	}
	result := r.db.Model(&model.ChatMessage{}).
		Where("id = ? AND user_id = ? AND session_id = ? AND role = ?", messageID, userID, sessionID, "assistant").
		Updates(map[string]any{"content": content, "retrieval_snapshot": snapshot, "model_name": modelName})
	return result.RowsAffected == 1, result.Error
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
