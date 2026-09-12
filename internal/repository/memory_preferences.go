package repository

import (
	"context"
	"gorm.io/gorm"
	"strconv"
	"strings"
	"time"
	"vid-lens/internal/model"
)

func structuredPreferenceKind(kind string) bool {
	return kind == "response.language" || kind == "response.verbosity" || kind == "response.format"
}

func legacyPreferenceKind(content string) string {
	switch content {
	case "回答语言：中文", "回答语言：英文":
		return "response.language"
	case "回答风格：简洁", "回答风格：详细":
		return "response.verbosity"
	case "回答格式：优先使用要点列表", "回答格式：优先使用段落":
		return "response.format"
	}
	return ""
}

// This compatibility migration changes only exact canonical legacy values.
// Unknown legacy content remains visible in governance, never guessed into prompts.
func (r *MemoryRepository) migrateLegacyPreferences(ctx context.Context, userID int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var items []model.AgentMemoryItem
		if err := tx.Where("user_id = ? AND scope_type = ? AND kind = ? AND status IN ?", userID, model.MemoryScopeUser, "response_preference", []string{model.MemoryStatusActive, model.MemoryStatusConflicted}).Find(&items).Error; err != nil {
			return err
		}
		legacyCounts := map[string]int{}
		for _, item := range items {
			legacyCounts[legacyPreferenceKind(item.Content)]++
		}
		for _, item := range items {
			kind := legacyPreferenceKind(item.Content)
			if kind == "" {
				continue
			}
			var count int64
			if err := tx.Model(&model.AgentMemoryItem{}).Where("user_id = ? AND scope_type = ? AND kind = ? AND status = ?", userID, model.MemoryScopeUser, kind, model.MemoryStatusActive).Count(&count).Error; err != nil {
				return err
			}
			status := model.MemoryStatusActive
			if count > 0 || legacyCounts[kind] > 1 {
				status = model.MemoryStatusConflicted
			}
			result := tx.Model(&model.AgentMemoryItem{}).Where("id = ? AND version = ? AND kind = ?", item.ID, item.Version, "response_preference").Updates(map[string]any{"kind": kind, "status": status, "version": item.Version + 1})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected > 0 {
				item.Version++
				if err := createMemoryEvent(tx, &item, "preference_migrated", item.SourceRef); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (r *MemoryRepository) ListStructuredPreferences(ctx context.Context, userID int64, now time.Time) ([]model.AgentMemoryItem, error) {
	if err := r.migrateLegacyPreferences(ctx, userID); err != nil {
		return nil, err
	}
	var items []model.AgentMemoryItem
	if err := r.db.WithContext(ctx).Where("user_id = ? AND scope_type = ? AND scope_id = ? AND kind IN ? AND status = ?", userID, model.MemoryScopeUser, strconv.FormatInt(userID, 10), []string{"response.language", "response.verbosity", "response.format"}, model.MemoryStatusActive).Where("expires_at IS NULL OR expires_at > ?", now).Order("kind ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return r.filterLiveMemorySources(ctx, userID, items)
}

func memoryMessageSourceID(ref string) int64 {
	for _, prefix := range []string{"chat_message:", "message:"} {
		if strings.HasPrefix(ref, prefix) {
			id, _ := strconv.ParseInt(strings.TrimPrefix(ref, prefix), 10, 64)
			return id
		}
	}
	return 0
}

func (r *MemoryRepository) filterLiveMemorySources(ctx context.Context, userID int64, items []model.AgentMemoryItem) ([]model.AgentMemoryItem, error) {
	result := make([]model.AgentMemoryItem, 0, len(items))
	for _, item := range items {
		// Canonical captured sources are always checked. Structured legacy sources
		// must also resolve; manual preferences retain their explicit provenance.
		if item.SourceType == "user_message" && (structuredPreferenceKind(item.Kind) || strings.HasPrefix(item.SourceRef, "chat_message:")) {
			id := memoryMessageSourceID(item.SourceRef)
			if id <= 0 {
				continue
			}
			var count int64
			if err := r.db.WithContext(ctx).Table("chat_messages AS cm").Joins("JOIN chat_sessions AS cs ON cs.id = cm.session_id AND cs.user_id = cm.user_id").Where("cm.id = ? AND cm.user_id = ? AND cm.role = ?", id, userID, "user").Count(&count).Error; err != nil {
				return nil, err
			}
			if count != 1 {
				continue
			}
		}
		result = append(result, item)
	}
	return result, nil
}
