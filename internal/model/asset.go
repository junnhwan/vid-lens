package model

import (
	"time"

	"gorm.io/gorm"
)

const (
	AssetLifecycleActive   = "active"
	AssetLifecycleDeleting = "deleting"
)

// VideoAsset 表示内容级视频文件资产。
// 同一个资产可以被多个用户任务复用，避免把文件唯一性和任务唯一性混在一起。
type VideoAsset struct {
	ID               int64          `gorm:"primaryKey;autoIncrement" json:"id"`
	FileMD5          string         `gorm:"type:char(32);uniqueIndex;not null" json:"file_md5"`
	ObjectName       string         `gorm:"type:varchar(500);not null" json:"object_name"`
	FileSize         int64          `gorm:"default:0" json:"file_size"`
	ContentType      string         `gorm:"type:varchar(100)" json:"content_type"`
	LifecycleState   string         `gorm:"type:varchar(20);not null;default:active;index" json:"lifecycle_state"`
	DeleteOwnerJobID *int64         `gorm:"index" json:"-"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`
}

func (VideoAsset) TableName() string {
	return "video_assets"
}
