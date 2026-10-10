package service

import (
	"context"
	"encoding/json"
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

type SourceRefreshRequest struct {
	ExpectedSourceID  *string `json:"expected_source_id"`
	TextSourcePolicy  string  `json:"text_source_policy,omitempty"`
	ProfileID         int64   `json:"profile_id,omitempty"`
	PreferredLanguage *string `json:"preferred_language,omitempty"`
	AutoSummary       *bool   `json:"auto_summary,omitempty"`
	ResumeASR         bool    `json:"resume_asr,omitempty"`
}
type SourceRefreshResult struct {
	TaskID       int64  `json:"task_id"`
	GenerationID string `json:"generation_id"`
	Operation    string `json:"operation"`
	Accepted     bool   `json:"accepted"`
}
type sourceRefreshKey struct{}

func WithSourceRefreshIdempotencyKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, sourceRefreshKey{}, key)
}

func (s *MediaService) RequestSourceRefresh(ctx context.Context, owner, taskID int64, key string, input SourceRefreshRequest) (*SourceRefreshResult, error) {
	if owner <= 0 || taskID <= 0 || input.ExpectedSourceID == nil || len(*input.ExpectedSourceID) > 36 || input.ProfileID < 0 {
		return nil, artifact.Err("invalid_source_refresh", 400)
	}
	if err := artifact.ValidateKey(key); err != nil {
		return nil, err
	}
	if input.TextSourcePolicy != "" && input.TextSourcePolicy != "prefer_platform" && input.TextSourcePolicy != "force_asr" {
		return nil, artifact.Err("invalid_processing_options", 400)
	}
	hash := processing.Fingerprint(struct {
		TaskID  int64
		Request SourceRefreshRequest
	}{taskID, input})
	return s.requestSourceRefresh(ctx, owner, taskID, key, input, "source_refresh", hash)
}

func (s *MediaService) requestSourceRefresh(ctx context.Context, owner, taskID int64, key string, input SourceRefreshRequest, action, hash string) (out *SourceRefreshResult, resultErr error) {
	result := func(gen string) *SourceRefreshResult {
		return &SourceRefreshResult{TaskID: taskID, GenerationID: gen, Operation: processing.OperationSourceRefresh, Accepted: true}
	}
	prior, err := s.repo.ImportRequest.Lookup(ctx, owner, action, key, hash)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		gen, err := s.repo.ImportRequest.ReadAcceptedGeneration(ctx, owner, action, key)
		return result(gen), err
	}
	ownedAcceptance := false
	// A same-key peer can commit between the initial receipt read and a
	// preflight check. Its immutable receipt wins over now-stale task/config
	// checks; an actual winning publisher still reports its dispatch failure.
	defer func() {
		if resultErr == nil || ownedAcceptance {
			return
		}
		prior, lookupErr := s.repo.ImportRequest.Lookup(ctx, owner, action, key, hash)
		if lookupErr == nil && prior != nil {
			gen, readErr := s.repo.ImportRequest.ReadAcceptedGeneration(ctx, owner, action, key)
			out, resultErr = result(gen), readErr
		}
	}()
	if err = s.requireV2GenerationAdmission(); err != nil {
		return nil, err
	}
	task, err := s.repo.Task.FindByID(taskID)
	if err != nil || task.UserID != owner {
		return nil, artifact.Err("not_found", 404)
	}
	old, err := processing.Decode(task.ProcessingIntentJSON)
	if err != nil {
		return nil, artifact.Err("source_refresh_requires_processing_intent", 422)
	}
	if task.ActiveTextSourceID != *input.ExpectedSourceID {
		return nil, artifact.Err("source_changed", 409)
	}
	if task.Status == model.TaskStatusQueued || task.Status == model.TaskStatusRunning {
		return nil, artifact.Err("task_processing_active", 409)
	}
	opts := old.Options
	opts.ProfileID = old.ProfileID
	if input.ProfileID > 0 {
		opts.ProfileID = input.ProfileID
	}
	if input.TextSourcePolicy != "" {
		opts.TextSourcePolicy = input.TextSourcePolicy
	}
	if input.PreferredLanguage != nil {
		opts.PreferredLanguage = *input.PreferredLanguage
	}
	if input.AutoSummary != nil {
		opts.AutoSummary = *input.AutoSummary
	}
	opts, err = processing.Normalize(opts, false)
	if err != nil {
		return nil, artifact.Err("invalid_processing_options", 400)
	}
	request := &preparedImport{options: opts}
	if err = s.freezeImport(owner, request); err != nil {
		return nil, err
	}
	if opts.AutoTagsEnabled && s.repo.UserTag != nil {
		state, stateErr := s.repo.UserTag.TaskState(ctx, owner, taskID)
		if stateErr != nil {
			return nil, stateErr
		}
		request.intent.ExpectedTagVersion = &state.Version
	}
	if opts.TextSourcePolicy == "force_asr" {
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
		if err = ai.RequireAction(*profile, "transcribe"); err != nil {
			return nil, artifact.Err("asr_profile_required", 422)
		}
	}
	if s.mq == nil {
		return nil, ErrTaskDispatchUnavailable
	}
	var prepared repository.InitialTaskDispatch
	frozen := processing.SourceRefreshSnapshot{Operation: processing.OperationSourceRefresh, Intent: request.intent, ExpectedActiveSourceID: task.ActiveTextSourceID, PreviousInputFingerprint: processing.SourceSummaryInputFingerprint(old), ClassificationChanged: processing.SourceClassificationFingerprint(old) != processing.SourceClassificationFingerprint(request.intent)}
	if input.ResumeASR && opts.TextSourcePolicy == "force_asr" && old.ProfileFingerprint == request.intent.ProfileFingerprint {
		// Initial/legacy accepted ASR used physical window keys without an
		// attempt namespace. Reuse them only for an explicitly compatible resume.
		frozen.ReuseUnscopedASRWindows = true
		priorJob, readErr := s.repo.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTranscribe)
		if readErr != nil {
			return nil, readErr
		}
		if priorJob != nil && priorJob.GenerationID == old.GenerationID {
			var previous processing.SourceRefreshSnapshot
			if json.Unmarshal([]byte(priorJob.InputSnapshotJSON), &previous) == nil && previous.Operation == processing.OperationSourceRefresh && previous.Intent.GenerationID == old.GenerationID {
				frozen.ReuseUnscopedASRWindows = previous.ReuseUnscopedASRWindows
				if !frozen.ReuseUnscopedASRWindows {
					frozen.ASRCheckpointGenerationID = previous.ASRCheckpointGenerationID
					if frozen.ASRCheckpointGenerationID == "" {
						frozen.ASRCheckpointGenerationID = old.GenerationID
					}
				}
			}
		}
	}
	_, created, err := s.repo.AcceptImport(ctx, owner, action, key, hash, func(tx *repository.Repositories) (*model.VideoTask, error) {
		now := time.Now()
		prepared, err = tx.PrepareSourceRefresh(ctx, repository.PrepareSourceRefreshRequest{UserID: owner, TaskID: taskID, ExpectedSourceID: task.ActiveTextSourceID, ExpectedIntentJSON: task.ProcessingIntentJSON, MediaFingerprint: task.FileMD5, Snapshot: frozen, Token: uuid.NewString(), Now: now, LeaseUntil: now.Add(initialDispatchLease)})
		return &prepared.Task, err
	})
	if err != nil {
		return nil, err
	}
	if !created {
		gen, err := s.repo.ImportRequest.ReadAcceptedGeneration(ctx, owner, action, key)
		return result(gen), err
	}
	ownedAcceptance = true
	ctx = mq.ContextWithClaimToken(mq.ContextWithRetryBudgetID(mq.ContextWithTraceID(ctx, prepared.Task.TraceID), prepared.RetryBudgetID), prepared.Token)
	kind, stage := model.TaskJobTypeTextSource, model.TaskStageTextSource
	if opts.TextSourcePolicy == "force_asr" {
		kind, stage = model.TaskJobTypeTranscribe, model.TaskStageTranscribing
		err = s.mq.EnqueueTranscribe(ctx, taskID, task.FileMD5)
	} else {
		if producer, ok := s.mq.(interface {
			EnqueueTextSource(context.Context, int64, string) error
		}); ok {
			err = producer.EnqueueTextSource(ctx, taskID, task.FileMD5)
		} else {
			err = ErrTaskDispatchUnavailable
		}
	}
	if err != nil {
		_, restoreErr := s.repo.RestoreRetryDispatch(repository.TaskDispatchRestoreRequest{TaskID: taskID, JobType: kind, Stage: stage, Token: prepared.Token, NextRetryAt: time.Now().Add(initialDispatchFailureBackoff), ErrorMessage: initialDispatchErrorMessage(err)})
		return nil, publicInitialDispatchError(ctx, prepared.Task, kind, stage, errors.Join(err, restoreErr))
	}
	return result(request.intent.GenerationID), nil
}

func v2ASRRefreshHash(taskID int64, force bool) string {
	return processing.Fingerprint(struct {
		TaskID int64
		Force  bool
	}{taskID, force})
}

func (s *MediaService) replayV2ASRRefresh(ctx context.Context, task *model.VideoTask, force bool) (bool, error) {
	key, _ := ctx.Value(sourceRefreshKey{}).(string)
	if key == "" {
		return false, nil
	}
	if err := artifact.ValidateKey(key); err != nil {
		return false, err
	}
	prior, err := s.repo.ImportRequest.Lookup(ctx, task.UserID, "source_refresh_asr", key, v2ASRRefreshHash(task.ID, force))
	return prior != nil, err
}

func (s *MediaService) requestV2ASRRefresh(ctx context.Context, task *model.VideoTask, force bool) error {
	key, _ := ctx.Value(sourceRefreshKey{}).(string)
	if err := artifact.ValidateKey(key); err != nil {
		return err
	}
	_, err := s.requestSourceRefresh(ctx, task.UserID, task.ID, key, SourceRefreshRequest{ExpectedSourceID: &task.ActiveTextSourceID, TextSourcePolicy: "force_asr", ResumeASR: !force}, "source_refresh_asr", v2ASRRefreshHash(task.ID, force))
	return err
}
