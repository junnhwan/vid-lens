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
		&AISummary{},
		&UserAIProfile{},
		&VideoChunk{},
		&VideoRAGIndex{},
		&KnowledgeBase{},
		&KnowledgeBaseVideo{},
		&ChatSession{},
		&ChatMessage{},
		&ChatMessageSource{},
		&AgentMemoryItem{},
		&AgentMemoryEvent{},
		&AgentMemoryPreference{},
		&AgentMemoryPolicyEvent{},
		&AgentRun{},
		&AgentStep{},
		&AgentToolCall{},
		&AgentClaim{},
		&AgentEvidence{},
		&AgentClaimEvidence{},
		&AICallLog{},
		&AIRetryBudget{},
		&AIRetryAttempt{},
		&AIUsageLedger{},
		&QuotaCompensation{},
		&UserUsageDaily{},
	}
}

// LegacyModels returns only the historical MySQL source schema copied by the
// offline mysql-to-postgres command. It intentionally excludes online-only
// knowledge-base tables and uses LegacyChatSession for chat_sessions.
func LegacyModels() []interface{} {
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
		&AISummary{},
		&UserAIProfile{},
		&VideoChunk{},
		&VideoRAGIndex{},
		&LegacyChatSession{},
		&ChatMessage{},
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
	if err := normalizeChatSessionScope(db); err != nil {
		return err
	}
	return normalizeChatSessionMemoryPolicy(db)
}

// MigrateLegacy upgrades only the offline historical MySQL source contract.
func MigrateLegacy(db *gorm.DB) error {
	return migrateModels(db, LegacyModels())
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
