package model

import (
	"time"
)

// VideoTranscription 视频转录明细表
// Transcription text is kept in a separate record because it can be much larger than task metadata.
// 用户刷历史列表时不需要加载庞大的文本内容
//
// file_md5 列承担内容+目标级去重（spec 03）：与 task_id 的 1:1 唯一索引解耦，
// 同一内容指纹跨 task/跨用户只允许一个成功转写结果行。本表无 status 列——
// 行存在即成功完成（spec 第 14 行"只复用 status=Completed 的成功结果"在此
// 落地为"行存在=成功"）。安全前提：失败 ASR 不向本表写行（失败状态只落在
// video_tasks 的 error_msg/last_error_*），故不存在"失败的转写行被误复用"。
// 故 spec 第 72 行的 partial unique index WHERE status=Completed 不适用——
// 直接 UNIQUE(file_md5) 即等价。这是"省 AI token"层的持久兜底，与文件层
// Asset file_md5 唯一索引、MQ 消息级 SETNX（spec 02）正交分工：文件层复用
// 资产省带宽、内容+目标层复用结果省 token、MQ 层去重重复投递保证 at-least-once
// 无副作用。
type VideoTranscription struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskID    int64     `gorm:"uniqueIndex;not null" json:"task_id"`
	FileMD5   string    `gorm:"type:char(32);not null;uniqueIndex:uk_video_transcriptions_file_md5" json:"file_md5"` // 内容指纹，跨 task 去重键
	Content   string    `gorm:"type:text" json:"content"`                                                            // 转录全文
	Words     int       `gorm:"default:0" json:"words"`                                                              // 字数统计
	CreatedAt time.Time `json:"created_at"`
}

func (VideoTranscription) TableName() string {
	return "video_transcriptions"
}
