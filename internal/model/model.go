package model

import "gorm.io/gorm"

// AllModels 返回所有需要自动迁移的模型
func AllModels() []interface{} {
	return []interface{}{
		&User{},
		&VideoAsset{},
		&VideoTask{},
		&VideoTranscription{},
		&VideoTranscriptionChunk{},
		&AISummary{},
		&UserAIProfile{},
		&VideoChunk{},
		&VideoRAGIndex{},
		&ChatSession{},
		&ChatMessage{},
	}
}

// Migrate 执行模型迁移，并兼容旧版本 video_tasks.file_md5 唯一索引。
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(AllModels()...); err != nil {
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
