package repository

import (
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"vid-lens/internal/model"
)

type AICallLogRepository struct {
	db *gorm.DB
}

func NewAICallLogRepository(db *gorm.DB) *AICallLogRepository {
	return &AICallLogRepository{db: db}
}

func (r *AICallLogRepository) Create(log *model.AICallLog) error {
	return r.db.Create(log).Error
}

func (r *AICallLogRepository) ListByUserID(userID int64, limit int) ([]model.AICallLog, error) {
	if limit <= 0 {
		limit = 50
	}
	var logs []model.AICallLog
	err := r.db.Where("user_id = ?", userID).
		Order("id desc").
		Limit(limit).
		Find(&logs).Error
	return logs, err
}

func (r *AICallLogRepository) FindDailyUsage(userID int64, date string) (*model.UserUsageDaily, error) {
	var usage model.UserUsageDaily
	err := r.db.Where("user_id = ? AND date = ?", userID, date).First(&usage).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &usage, nil
}

func (r *AICallLogRepository) IncrementDailyUsage(userID int64, date, kind, status string, inputChars, outputChars, asrSeconds int) error {
	usage := model.UserUsageDaily{
		UserID:      userID,
		Date:        date,
		InputChars:  inputChars,
		OutputChars: outputChars,
	}
	updates := map[string]interface{}{
		"input_chars":  gorm.Expr("input_chars + ?", inputChars),
		"output_chars": gorm.Expr("output_chars + ?", outputChars),
	}
	switch kind {
	case model.AICallKindASR:
		usage.ASRRequests = 1
		usage.ASRSeconds = asrSeconds
		updates["asr_requests"] = gorm.Expr("asr_requests + ?", 1)
		updates["asr_seconds"] = gorm.Expr("asr_seconds + ?", asrSeconds)
	case model.AICallKindLLM:
		usage.LLMRequests = 1
		updates["llm_requests"] = gorm.Expr("llm_requests + ?", 1)
	case model.AICallKindEmbedding:
		usage.EmbeddingRequests = 1
		updates["embedding_requests"] = gorm.Expr("embedding_requests + ?", 1)
	}
	if status == model.AICallStatusFailed {
		usage.FailedRequests = 1
		updates["failed_requests"] = gorm.Expr("failed_requests + ?", 1)
	}

	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "date"}},
		DoUpdates: clause.Assignments(updates),
	}).Create(&usage).Error
}
