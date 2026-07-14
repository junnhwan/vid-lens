package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/observability"
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
	if o == nil || o.repos == nil || o.repos.AICallLog == nil {
		return nil
	}
	correlation := observability.CorrelationFromContext(ctx)
	if record.TraceID == "" {
		record.TraceID = correlation.TraceID
	}
	if record.TaskID == 0 {
		record.TaskID = correlation.TaskID
	}
	if record.JobID == 0 {
		record.JobID = correlation.JobID
	}
	if record.UserID == 0 {
		record.UserID = correlation.UserID
	}
	if record.JobType == "" {
		record.JobType = correlation.JobType
	}
	if record.Stage == "" {
		record.Stage = correlation.Stage
	}
	if record.Attempt == 0 {
		record.Attempt = correlation.Attempt
	}
	if record.UserID <= 0 {
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
		UserID: record.UserID, TaskID: record.TaskID, JobID: record.JobID, SessionID: record.SessionID,
		TraceID: strings.TrimSpace(record.TraceID), JobType: strings.TrimSpace(record.JobType), Stage: strings.TrimSpace(record.Stage), Attempt: record.Attempt,
		Kind: strings.TrimSpace(record.Kind), Provider: strings.TrimSpace(record.Provider), ModelName: strings.TrimSpace(record.Model), Status: record.Status,
		DurationMs: record.DurationMs, InputChars: record.InputChars, OutputChars: record.OutputChars,
		PromptTokens: record.PromptTokens, CompletionTokens: record.CompletionTokens, TotalTokens: record.TotalTokens,
		EstimatedCost: record.EstimatedCost, TokenEstimated: record.TokenEstimated, Currency: strings.TrimSpace(record.Currency),
		PriceVersion: strings.TrimSpace(record.PriceVersion), ProviderRequestID: strings.TrimSpace(record.ProviderRequestID),
		ErrorCode: strings.TrimSpace(record.ErrorCode), ErrorMsg: truncateAICallError(record.ErrorMsg), CreatedAt: now(),
	}
	if err := o.repos.AICallLog.Create(log); err != nil {
		return err
	}
	return o.repos.AICallLog.IncrementDailyUsage(record.UserID, log.CreatedAt.Format("2006-01-02"), log.Kind, log.Status, log.InputChars, log.OutputChars, record.ASRSeconds)
}
func truncateAICallError(errMsg string) string {
	if strings.TrimSpace(errMsg) == "" {
		return ""
	}
	return observability.SafeError(errors.New(strings.TrimSpace(errMsg)))
}
