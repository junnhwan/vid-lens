package model

import "time"

// UploadSessionChunk records the one accepted content identity for a chunk
// index. Its MinIO object name is content-addressed, so conflicting retries
// cannot overwrite accepted bytes.
type UploadSessionChunk struct {
	ID            int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID     string    `gorm:"type:varchar(36);not null;uniqueIndex:idx_upload_session_chunk" json:"session_id"`
	ChunkIndex    int       `gorm:"not null;uniqueIndex:idx_upload_session_chunk" json:"chunk_index"`
	ActualSize    int64     `gorm:"not null" json:"actual_size"`
	ContentSHA256 string    `gorm:"type:char(64);not null" json:"content_sha256"`
	ObjectName    string    `gorm:"type:varchar(500);not null" json:"object_name"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (UploadSessionChunk) TableName() string {
	return "upload_session_chunks"
}
