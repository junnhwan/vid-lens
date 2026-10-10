package service

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"time"
	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/mq"
	"vid-lens/internal/processing"
	"vid-lens/internal/repository"
)

type SummaryVisualRetryRequest struct {
	ExpectedGenerationID     string `json:"expected_generation_id"`
	ExpectedGeneratedVersion int64  `json:"expected_generated_version"`
	ExpectedContentDigest    string `json:"expected_content_digest"`
	ExpectedSourceID         string `json:"expected_source_id"`
	ExpectedSourceDigest     string `json:"expected_source_digest"`
	ProfileID                int64  `json:"profile_id,omitempty"`
	OutputMode               string `json:"output_mode,omitempty"`
	AuthorizeNewVisualBudget bool   `json:"authorize_new_visual_budget"`
}
type SummaryVisualRetryResult struct {
	TaskID             int64  `json:"task_id"`
	GenerationID       string `json:"generation_id"`
	ParentGenerationID string `json:"parent_generation_id"`
	Operation          string `json:"operation"`
	Accepted           bool   `json:"accepted"`
}

func (s *MediaService) RequestSummaryVisualRetry(ctx context.Context, owner, taskID int64, key string, input SummaryVisualRetryRequest) (*SummaryVisualRetryResult, error) {
	if taskID <= 0 || owner <= 0 || input.ExpectedGenerationID == "" || len(input.ExpectedGenerationID) > 36 || input.ExpectedGeneratedVersion <= 0 || len(input.ExpectedContentDigest) != 64 || input.ExpectedSourceID == "" || len(input.ExpectedSourceDigest) != 64 || input.ProfileID < 0 || !input.AuthorizeNewVisualBudget {
		return nil, artifact.Err("invalid_visual_retry_request", 400)
	}
	if input.OutputMode != "" && input.OutputMode != "auto" && input.OutputMode != "image_text" && input.OutputMode != "keyframes" {
		return nil, artifact.Err("invalid_processing_options", 400)
	}
	if err := artifact.ValidateKey(key); err != nil {
		return nil, err
	}
	action := "summary_visual_retry"
	hash := processing.Fingerprint(struct {
		TaskID  int64
		Request SummaryVisualRetryRequest
	}{taskID, input})
	result := func(gen string) *SummaryVisualRetryResult {
		return &SummaryVisualRetryResult{TaskID: taskID, GenerationID: gen, ParentGenerationID: input.ExpectedGenerationID, Operation: processing.OperationVisualRetry, Accepted: true}
	}
	prior, err := s.repo.ImportRequest.Lookup(ctx, owner, action, key, hash)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		gen, err := s.repo.ImportRequest.ReadAcceptedGeneration(ctx, owner, action, key)
		return result(gen), err
	}
	if err = s.requireV2GenerationAdmission(); err != nil {
		return nil, err
	}
	if !s.cfg.SummaryExperience.VisualEnabled() {
		return nil, artifact.Err("visual_disabled", 422)
	}
	task, err := s.repo.Task.FindByID(taskID)
	if err != nil || task.UserID != owner {
		return nil, artifact.Err("not_found", 404)
	}
	if !task.VisualCaptionAllowed() || task.EffectiveVisualMode() == model.VisualModeOff {
		return nil, artifact.Err("visual_disabled", 422)
	}
	job, err := s.repo.TaskJob.FindByTaskAndType(taskID, model.TaskJobTypeSummary)
	if err != nil {
		return nil, err
	}
	if repository.SummaryJobActive(job) {
		return nil, artifact.Err("summary_generation_active", 409)
	}
	old, err := processing.Decode(task.ProcessingIntentJSON)
	if err != nil {
		return nil, artifact.Err("invalid_generation_snapshot", 409)
	}
	options := old.Options
	options.AutoSummary = true
	options.SummaryVisualEnabled = true
	options.ProfileID = old.ProfileID
	if input.ProfileID > 0 {
		options.ProfileID = input.ProfileID
	}
	if input.OutputMode != "" {
		options.OutputMode = input.OutputMode
	} else if options.OutputMode == "text" {
		options.OutputMode = "image_text"
	}
	options, err = processing.Normalize(options, false)
	if err != nil {
		return nil, artifact.Err("invalid_processing_options", 400)
	}
	request := &preparedImport{options: options}
	if err = s.freezeImport(owner, request); err != nil {
		return nil, err
	}
	provider, ok := s.profiles.(interface {
		GetAIProfileByID(int64, int64) (*ai.Profile, error)
	})
	if !ok {
		return nil, artifact.Err("profile_required", 422)
	}
	profile, err := provider.GetAIProfileByID(owner, request.intent.ProfileID)
	if err != nil || profile == nil || processing.FingerprintProfile(*profile) != request.intent.ProfileFingerprint {
		return nil, artifact.Err("frozen_profile_changed", 409)
	}
	if !ai.VisionConfigured(*profile) {
		return nil, artifact.Err("vision_unavailable", 422)
	}
	if s.mq == nil {
		return nil, ErrTaskDispatchUnavailable
	}
	var prepared repository.InitialTaskDispatch
	_, created, err := s.repo.AcceptImport(ctx, owner, action, key, hash, func(tx *repository.Repositories) (*model.VideoTask, error) {
		now := time.Now()
		prepared, err = tx.PrepareSummaryVisualRetry(ctx, repository.PrepareSummaryVisualRetryRequest{UserID: owner, TaskID: taskID, ExpectedGenerationID: input.ExpectedGenerationID, ExpectedGeneratedVersion: input.ExpectedGeneratedVersion, ExpectedContentDigest: input.ExpectedContentDigest, ExpectedSourceID: input.ExpectedSourceID, ExpectedSourceDigest: input.ExpectedSourceDigest, ExpectedIntentJSON: task.ProcessingIntentJSON, Intent: request.intent, Token: uuid.NewString(), Now: now, LeaseUntil: now.Add(initialDispatchLease)})
		return &prepared.Task, err
	})
	if err != nil {
		return nil, err
	}
	if !created {
		gen, err := s.repo.ImportRequest.ReadAcceptedGeneration(ctx, owner, action, key)
		return result(gen), err
	}
	enqueueCtx := mq.ContextWithClaimToken(mq.ContextWithRetryBudgetID(mq.ContextWithTraceID(ctx, prepared.Task.TraceID), prepared.RetryBudgetID), prepared.Token)
	if err = s.mq.EnqueueSummary(enqueueCtx, taskID, task.FileMD5); err != nil {
		_, restoreErr := s.repo.RestoreRetryDispatch(repository.TaskDispatchRestoreRequest{TaskID: taskID, JobType: model.TaskJobTypeSummary, Stage: model.TaskStageSummarizing, Token: prepared.Token, NextRetryAt: time.Now().Add(initialDispatchFailureBackoff), ErrorMessage: initialDispatchErrorMessage(err)})
		return nil, publicInitialDispatchError(ctx, prepared.Task, model.TaskJobTypeSummary, model.TaskStageSummarizing, errors.Join(err, restoreErr))
	}
	return result(request.intent.GenerationID), nil
}
