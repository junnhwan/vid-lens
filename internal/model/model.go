package model

import "gorm.io/gorm"

// AllModels returns the complete PostgreSQL online schema.
func AllModels() []interface{} {
	return []interface{}{
		&User{},
		&VideoAsset{},
		&VideoTask{},
		&TaskJob{},
		&TaskCleanupJob{},
		&KafkaMessageFailure{},
		&VideoTranscription{},
		&VideoTranscriptionChunk{},
		&VideoVisualFrame{},
		&VideoVisualObservation{},
		&AISummary{},
		&SummaryPart{},
		&UserAIProfile{},
		&UserPromptPreference{},
		&VideoChunk{},
		&VideoRAGIndex{},
		&KnowledgeBase{},
		&KnowledgeBaseVideo{},
		&ChatSession{},
		&ChatMessage{},
		&ChatFeedback{},
		&ChatMessageSource{},
		&AgentMemoryItem{},
		&MemoryCaptureJob{},
		&AgentMemoryEvent{},
		&AgentMemoryPreference{},
		&AgentMemoryPolicyEvent{},
		&AgentRun{},
		&AgentStep{},
		&AgentToolCall{},
		&AICallLog{},
		&AIRetryBudget{},
		&AIRetryAttempt{},
		&AIUsageLedger{},
		&QuotaCompensation{},
		&UserUsageDaily{},
	}
}

// Migrate executes the complete online schema migration.
func Migrate(db *gorm.DB) error {
	if err := normalizeChatSessionScope(db); err != nil {
		return err
	}
	if err := normalizeChatSessionMemoryPolicy(db); err != nil {
		return err
	}
	if err := migrateModels(db, AllModels()); err != nil {
		return err
	}
	if db.Dialector.Name() == "postgres" {
		if err := db.Exec("ALTER TABLE chat_sessions DROP CONSTRAINT IF EXISTS chk_chat_sessions_scope").Error; err != nil {
			return err
		}
		if err := db.Exec("ALTER TABLE chat_sessions ADD CONSTRAINT chk_chat_sessions_scope CHECK ((scope_type = 'video' AND task_id > 0 AND knowledge_base_id = 0) OR (scope_type = 'knowledge_base' AND task_id = 0 AND knowledge_base_id > 0) OR (scope_type = 'video_library' AND task_id = 0 AND knowledge_base_id = 0))").Error; err != nil {
			return err
		}
	}
	if err := normalizeChatSessionScope(db); err != nil {
		return err
	}
	return normalizeChatSessionMemoryPolicy(db)
}

func migrateModels(db *gorm.DB, models []interface{}) error {
	if err := db.AutoMigrate(models...); err != nil {
		return err
	}

	if db.Migrator().HasIndex(&VideoTask{}, "idx_file_md5") {
		if err := db.Migrator().DropIndex(&VideoTask{}, "idx_file_md5"); err != nil {
			return err
		}
	}
	if !db.Migrator().HasIndex(&VideoTask{}, "idx_video_tasks_file_md5") {
		if err := db.Migrator().CreateIndex(&VideoTask{}, "FileMD5"); err != nil {
			return err
		}
	}

	return nil
}

func normalizeChatSessionScope(db *gorm.DB) error {
	if !db.Migrator().HasTable(&ChatSession{}) || !db.Migrator().HasColumn(&ChatSession{}, "scope_type") {
		return nil
	}
	updates := map[string]any{"scope_type": ChatScopeVideo}
	if db.Migrator().HasColumn(&ChatSession{}, "knowledge_base_id") {
		updates["knowledge_base_id"] = 0
	}
	return db.Table("chat_sessions").
		Where("scope_type IS NULL OR scope_type = ''").
		Updates(updates).Error
}

func normalizeChatSessionMemoryPolicy(db *gorm.DB) error {
	if !db.Migrator().HasTable(&ChatSession{}) || !db.Migrator().HasColumn(&ChatSession{}, "memory_policy") {
		return nil
	}
	updates := map[string]any{"memory_policy": MemorySessionPolicyInherit}
	if db.Migrator().HasColumn(&ChatSession{}, "memory_policy_version") {
		updates["memory_policy_version"] = 0
	}
	return db.Table("chat_sessions").
		Where("memory_policy IS NULL OR memory_policy = ''").
		Updates(updates).Error
}
