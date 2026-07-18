package model

import (
	"time"
)

// VideoTranscription 视频转录明细表
// 面试亮点：垂直拆分思想 —— 逐字稿可能数万字，拆出主表保证查询性能
// 用户刷历史列表时不需要加载庞大的文本内容
type VideoTranscription struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskID    int64     `gorm:"uniqueIndex;not null" json:"task_id"`
	Content   string    `gorm:"type:text" json:"content"` // 转录全文
	Words     int       `gorm:"default:0" json:"words"`   // 字数统计
	CreatedAt time.Time `json:"created_at"`
}

func (VideoTranscription) TableName() string {
	return "video_transcriptions"
}
