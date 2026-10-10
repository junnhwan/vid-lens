package model

import "gorm.io/gorm"

// AllModels returns the complete PostgreSQL online schema.
func AllModels() []interface{} {
	return []interface{}{
		&User{},
		&VideoAsset{},
		&VideoTask{},
		&ImportRequest{},
		&TaskJob{},
		&TaskCleanupJob{},
		&KafkaMessageFailure{},
		&VideoTranscription{},
		&VideoTextSource{}, &VideoTextCue{},
		&VideoTranscriptionChunk{},
		&VideoVisualFrame{},
		&VideoVisualProgress{},
		&VideoVisualObservation{},
		&AISummary{},
		&SummaryScreenshotRef{},
		&SummaryRevisionHead{}, &SummaryRevision{}, &SummaryEditOperation{}, &SummaryEditDispatch{},
		&VideoTermRuleHead{}, &VideoTermRuleVersion{},
		&UserTag{}, &UserTagName{}, &UserTagVocabularyHead{}, &VideoTagSetHead{}, &VideoTagAssignment{}, &VideoTagDecision{}, &VideoTagSuggestion{}, &UserTagMergeRecord{},
		&VideoTagClassification{},
		&SummaryTagIntent{},
		&SummaryPart{},
		&UserAIProfile{},
		&HostedAIConfig{},
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
		&Artifact{}, &ArtifactVersion{}, &SourceManifest{}, &SourceSnapshotItem{},
		&ArtifactCanvasLayout{},
		&ArtifactEvidenceRef{}, &GenerationRequest{}, &GenerationDispatch{}, &ArtifactEditDispatch{}, &RunEvent{},
		&ArtifactEditRequest{}, &ArtifactEditOperation{}, &ArtifactEditOutcome{},
		&LearningPosition{}, &AnswerImport{},
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
	if err := migrateArtifactSubjects(db); err != nil {
		return err
	}
	if err := migrateArtifactEditDispatches(db); err != nil {
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
	if db.Migrator().HasIndex(&VideoTranscription{}, "uk_video_transcriptions_file_md5") {
		if err := db.Migrator().DropIndex(&VideoTranscription{}, "uk_video_transcriptions_file_md5"); err != nil {
			return err
		}
	}
	// A forced summary on a deduplicated task needs its own row. Keep the
	// file hash indexed for reuse, while task_id remains unique.
	if db.Migrator().HasIndex(&AISummary{}, "uk_ai_summaries_file_md5") {
		if err := db.Migrator().DropIndex(&AISummary{}, "uk_ai_summaries_file_md5"); err != nil {
			return err
		}
	}
	// Older schemas made one content hash/model globally unique. Each user's
	// task needs its own authorized chunk projection, even for identical media.
	if db.Migrator().HasIndex(&VideoRAGIndex{}, "uk_rag_file_md5_model") {
		if err := db.Migrator().DropIndex(&VideoRAGIndex{}, "uk_rag_file_md5_model"); err != nil {
			return err
		}
	}
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
