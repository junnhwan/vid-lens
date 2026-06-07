package service

import (
	"context"
	"strings"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

type AIObserver struct {
	repos *repository.Repositories
	now   func() time.Time
}

func NewAIObserver(repos *repository.Repositories) *AIObserver {
	return &AIObserver{repos: repos, now: time.Now}
}

func (o *AIObserver) RecordAICall(ctx context.Context, record ai.CallRecord) error {
	_ = ctx
	if o == nil || o.repos == nil || o.repos.AICallLog == nil || record.UserID <= 0 {
		return nil
	}
	now := o.now
	if now == nil {
		now = time.Now
	}
	if record.Status == "" {
		record.Status = model.AICallStatusSuccess
	}
	log := &model.AICallLog{
		UserID:      record.UserID,
		TaskID:      record.TaskID,
		SessionID:   record.SessionID,
		Kind:        strings.TrimSpace(record.Kind),
		Provider:    strings.TrimSpace(record.Provider),
		ModelName:   strings.TrimSpace(record.Model),
		Status:      record.Status,
		DurationMs:  record.DurationMs,
		InputChars:  record.InputChars,
		OutputChars: record.OutputChars,
		ErrorCode:   strings.TrimSpace(record.ErrorCode),
		ErrorMsg:    truncateAICallError(record.ErrorMsg),
		CreatedAt:   now(),
	}
	if err := o.repos.AICallLog.Create(log); err != nil {
		return err
	}
	return o.repos.AICallLog.IncrementDailyUsage(
		record.UserID,
		log.CreatedAt.Format("2006-01-02"),
		log.Kind,
		log.Status,
		log.InputChars,
		log.OutputChars,
		record.ASRSeconds,
	)
}

func truncateAICallError(errMsg string) string {
	errMsg = strings.TrimSpace(errMsg)
	if len(errMsg) > 500 {
		return errMsg[:500]
	}
	return errMsg
}
