package model

import (
	"time"
)

// VideoTranscription 视频转录明细表
// Transcription text is kept in a separate record because it can be much larger than task metadata.
// 用户刷历史列表时不需要加载庞大的文本内容
//
// file_md5 is indexed for cross-task reuse. A forced transcription owns a
// separate result by task_id and must not overwrite another user's result.
// A row exists only after ASR succeeds; failed attempts live on the task.
type VideoTranscription struct {
	SourceID     string    `gorm:"type:varchar(36);not null;default:'';index" json:"source_id,omitempty"`
	SourceKind   string    `gorm:"type:varchar(32);not null;default:''" json:"source_kind,omitempty"`
	SourceDigest string    `gorm:"type:char(64);not null;default:''" json:"source_digest,omitempty"`
	ID           int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskID       int64     `gorm:"uniqueIndex;not null" json:"task_id"`
	FileMD5      string    `gorm:"type:char(32);not null;index:idx_video_transcriptions_file_md5" json:"file_md5"` // 内容指纹，跨 task 复用键
	Content      string    `gorm:"type:text" json:"content"`                                                       // 转录全文
	Words        int       `gorm:"default:0" json:"words"`                                                         // 字数统计
	CreatedAt    time.Time `json:"created_at"`
}

func (VideoTranscription) TableName() string {
	return "video_transcriptions"
}
