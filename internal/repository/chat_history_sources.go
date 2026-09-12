package repository

import (
	"context"
	"vid-lens/internal/model"
)

// Do not join current task/membership tables here: missing source edges would
// hide that a historic answer depended on a removed video.
func (r *ChatRepository) ListMessageSourcesForUser(ctx context.Context, userID, sessionID int64, messageIDs []int64) ([]model.ChatMessageSource, error) {
	sources := []model.ChatMessageSource{}
	if len(messageIDs) == 0 {
		return sources, nil
	}
	err := r.db.WithContext(ctx).Table("chat_message_sources AS cms").Select("cms.*").Joins("JOIN chat_messages AS cm ON cm.id = cms.message_id AND cm.session_id = cms.session_id").Where("cm.user_id = ? AND cm.session_id = ? AND cm.role = ? AND cm.id IN ?", userID, sessionID, "assistant", messageIDs).Order("cms.message_id ASC, cms.task_id ASC").Scan(&sources).Error
	return sources, err
}
