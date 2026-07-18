package repository

import (
	"errors"
	"fmt"
	"vid-lens/internal/model"
)

var ErrInitialTaskDispatchConflict = errors.New("initial task dispatch state changed")

// PrepareInitialTaskDispatch persists the task, child job, retry budget and
// initial dispatch lease in one PostgreSQL transaction. Kafka publishing must
// happen only after this method commits. The lease makes a crash between commit
// and publish recoverable by RetryScheduler after LeaseUntil.
func (r *Repositories) PrepareInitialTaskDispatch(req InitialTaskDispatchRequest) (InitialTaskDispatch, error) {
	if err := validateInitialTaskDispatchRequest(req); err != nil {
		return InitialTaskDispatch{}, err
	}

	var prepared InitialTaskDispatch
	err := r.Transaction(func(repos *Repositories) error {
		task, err := repos.prepareInitialDispatchTask(req)
		if err != nil {
			return err
		}
		if err := repos.TaskJob.UpsertQueued(task, req.JobType, req.Stage, task.MaxRetries); err != nil {
			return fmt.Errorf("prepare initial task job: %w", err)
		}
		budgetID, err := repos.ensureTaskJobRetryBudget(task.ID, req.JobType, req.Now)
		if err != nil {
			return fmt.Errorf("prepare initial retry budget: %w", err)
		}
		prepared = InitialTaskDispatch{Task: *task, RetryBudgetID: budgetID, Token: req.Token}
		return nil
	})
	if err != nil {
		return InitialTaskDispatch{}, err
	}
	// Do not mutate caller-owned state before commit; a failed create may have
	// assigned a transient ID even though PostgreSQL rolled the row back.
	*req.Task = prepared.Task
	return prepared, nil
}

func validateInitialTaskDispatchRequest(req InitialTaskDispatchRequest) error {
	if req.Task == nil || req.JobType == "" || req.Stage == "" || req.Token == "" {
		return fmt.Errorf("initial task dispatch parameters are incomplete")
	}
	if req.Now.IsZero() || !req.LeaseUntil.After(req.Now) {
		return fmt.Errorf("initial task dispatch lease is invalid")
	}
	if req.CreateTask {
		if req.Task.ID != 0 {
			return fmt.Errorf("new initial task dispatch already has an id")
		}
		return nil
	}
	if req.Task.ID <= 0 || len(req.AllowedStatuses) == 0 {
		return fmt.Errorf("existing initial task dispatch requires id and allowed statuses")
	}
	return nil
}

func (r *Repositories) prepareInitialDispatchTask(req InitialTaskDispatchRequest) (*model.VideoTask, error) {
	if req.CreateTask {
		task := *req.Task
		applyInitialDispatchState(&task, req, 1)
		if task.MaxRetries <= 0 {
			task.MaxRetries = 3
		}
		if err := r.Task.Create(&task); err != nil {
			return nil, fmt.Errorf("create initial dispatch task: %w", err)
		}
		return &task, nil
	}

	current, err := r.Task.FindByID(req.Task.ID)
	if err != nil {
		return nil, err
	}
	if req.Task.UserID > 0 && current.UserID != req.Task.UserID {
		return nil, ErrInitialTaskDispatchConflict
	}
	newVersion := current.LeaseVersion + 1
	result := r.db.Model(&model.VideoTask{}).
		Where("id = ? AND status IN ? AND lease_version = ?", current.ID, req.AllowedStatuses, current.LeaseVersion).
		Updates(initialDispatchTaskUpdates(req, newVersion))
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, ErrInitialTaskDispatchConflict
	}
	applyInitialDispatchState(current, req, newVersion)
	return current, nil
}

func initialDispatchTaskUpdates(req InitialTaskDispatchRequest, leaseVersion int64) map[string]interface{} {
	return map[string]interface{}{
		"status":            model.TaskStatusQueued,
		"stage":             req.Stage,
		"last_job_type":     req.JobType,
		"retry_count":       0,
		"next_retry_at":     nil,
		"error_msg":         "",
		"last_error_code":   "",
		"last_error_msg":    "",
		"processing_token":  req.Token,
		"lease_kind":        model.TaskLeaseKindDispatch,
		"lease_expires_at":  req.LeaseUntil,
		"lease_version":     leaseVersion,
		"stage_started_at":  req.Now,
		"stage_finished_at": nil,
		"finished_at":       nil,
	}
}

func applyInitialDispatchState(task *model.VideoTask, req InitialTaskDispatchRequest, leaseVersion int64) {
	leaseUntil := req.LeaseUntil
	stageStartedAt := req.Now
	task.Status = model.TaskStatusQueued
	task.Stage = req.Stage
	task.LastJobType = req.JobType
	task.RetryCount = 0
	task.NextRetryAt = nil
	task.ErrorMsg = ""
	task.LastErrorCode = ""
	task.LastErrorMsg = ""
	task.ProcessingToken = req.Token
	task.LeaseKind = model.TaskLeaseKindDispatch
	task.LeaseExpiresAt = &leaseUntil
	task.LeaseVersion = leaseVersion
	task.StageStartedAt = &stageStartedAt
	task.StageFinishedAt = nil
	task.FinishedAt = nil
}
