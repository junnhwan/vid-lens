package model

// AllModels 返回所有需要自动迁移的模型
func AllModels() []interface{} {
	return []interface{}{
		&User{},
		&VideoTask{},
		&VideoTranscription{},
		&AISummary{},
	}
}
