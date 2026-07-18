package repository

import (
	"errors"
	"time"

	"vid-lens/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TaskJobRepository struct {
	db *gorm.DB
}

func NewTaskJobRepository(db *gorm.DB) *TaskJobRepository {
	return &TaskJobRepository{db: db}
}

func (r *TaskJobRepository) UpsertQueued(task *model.VideoTask, jobType, stage string, maxRetries int) error {
	return r.upsertDispatchState(task, jobType, model.TaskStatusQueued, stage, maxRetries, true)
}

func (r *TaskJobRepository) UpsertDispatching(task *model.VideoTask, jobType string, status int8, stage string) error {
	return r.upsertDispatchState(task, jobType, status, stage, task.MaxRetries, false)
}

func (r *TaskJobRepository) upsertDispatchState(task *model.VideoTask, jobType string, status int8, stage string, maxRetries int, resetRetry bool) error {
	if task == nil {
		return errors.New("task is nil")
	}
	if maxRetries <= 0 {
		maxRetries = task.MaxRetries
	}
	if maxRetries <= 0 {
		maxRetries = 3
	}
	// A TaskJob owns its stage retry budget. A newly created stage always
	// starts at zero and never inherits the compatibility mirror on VideoTask.
	retryCount := 0
	job := &model.TaskJob{
		TaskID:          task.ID,
		UserID:          task.UserID,
		JobType:         jobType,
		Status:          status,
		Stage:           stage,
		TraceID:         task.TraceID,
		RetryCount:      retryCount,
		MaxRetries:      maxRetries,
		ProcessingToken: task.ProcessingToken,
		LeaseKind:       task.LeaseKind,
		LeaseExpiresAt:  task.LeaseExpiresAt,
		LeaseVersion:    task.LeaseVersion,
	}
	updates := map[string]interface{}{
		"user_id":          task.UserID,
		"status":           status,
		"stage":            stage,
		"trace_id":         task.TraceID,
		"max_retries":      maxRetries,
		"next_retry_at":    nil,
		"last_error_code":  "",
		"last_error_msg":   "",
		"processing_token": task.ProcessingToken,
		"lease_kind":       task.LeaseKind,
		"lease_expires_at": task.LeaseExpiresAt,
		"lease_version":    task.LeaseVersion,
		"started_at":       nil,
		"finished_at":      nil,
	}
	if resetRetry {
		updates["retry_count"] = 0
		// An explicit user submission starts a new retry cycle. Scheduler
		// redispatches never call UpsertQueued, so they keep the same budget.
		updates["retry_budget_id"] = ""
		updates["retry_budget_generation"] = gorm.Expr("task_jobs.retry_budget_generation + 1")
	}
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "task_id"}, {Name: "job_type"}},
		DoUpdates: clause.Assignments(updates),
	}).Create(job).Error
}

func (r *TaskJobRepository) FindByTaskAndType(taskID int64, jobType string) (*model.TaskJob, error) {
	var job model.TaskJob
	err := r.db.Where("task_id = ? AND job_type = ?", taskID, jobType).First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *TaskJobRepository) ListByTaskID(userID, taskID int64) ([]model.TaskJob, error) {
	var jobs []model.TaskJob
	err := r.db.Where("user_id = ? AND task_id = ?", userID, taskID).
		Order("created_at ASC").
		Find(&jobs).Error
	return jobs, err
}

func (r *TaskJobRepository) MarkRunning(taskID int64, jobType, stage string) error {
	now := time.Now()
	return r.db.Model(&model.TaskJob{}).
		Where("task_id = ? AND job_type = ?", taskID, jobType).
		Updates(map[string]interface{}{
			"status":          model.TaskStatusRunning,
			"stage":           stage,
			"next_retry_at":   nil,
			"last_error_code": "",
			"last_error_msg":  "",
			"started_at":      &now,
			"finished_at":     nil,
		}).Error
}

func (r *TaskJobRepository) MarkCompleted(taskID int64, jobType, stage string) error {
	now := time.Now()
	return r.db.Model(&model.TaskJob{}).
		Where("task_id = ? AND job_type = ?", taskID, jobType).
		Updates(map[string]interface{}{
			"status":          model.TaskStatusCompleted,
			"stage":           stage,
			"next_retry_at":   nil,
			"last_error_code": "",
			"last_error_msg":  "",
			"finished_at":     &now,
		}).Error
}

func (r *TaskJobRepository) RecordRetryableFailure(taskID int64, jobType, stage, errMsg string, retryCount, maxRetries int, nextRetryAt time.Time) error {
	if err := r.ensureJob(taskID, jobType, stage, maxRetries); err != nil {
		return err
	}
	now := time.Now()
	return r.db.Model(&model.TaskJob{}).
		Where("task_id = ? AND job_type = ?", taskID, jobType).
		Updates(map[string]interface{}{
			"status":          model.TaskStatusFailed,
			"stage":           stage,
			"retry_count":     retryCount,
			"max_retries":     maxRetries,
			"next_retry_at":   nextRetryAt,
			"last_error_code": "retryable_error",
			"last_error_msg":  errMsg,
			"finished_at":     &now,
		}).Error
}

func (r *TaskJobRepository) RecordTerminalFailure(taskID int64, jobType, stage, errCode, errMsg string, retryCount, maxRetries int, status int8) error {
	if err := r.ensureJob(taskID, jobType, stage, maxRetries); err != nil {
		return err
	}
	now := time.Now()
	return r.db.Model(&model.TaskJob{}).
		Where("task_id = ? AND job_type = ?", taskID, jobType).
		Updates(map[string]interface{}{
			"status":          status,
			"stage":           stage,
			"retry_count":     retryCount,
			"max_retries":     maxRetries,
			"next_retry_at":   nil,
			"last_error_code": errCode,
			"last_error_msg":  errMsg,
			"finished_at":     &now,
		}).Error
}

func (r *TaskJobRepository) DeleteByTaskID(taskID int64) error {
	return r.db.Where("task_id = ?", taskID).Delete(&model.TaskJob{}).Error
}

func (r *TaskJobRepository) ensureJob(taskID int64, jobType, stage string, maxRetries int) error {
	var existing model.TaskJob
	err := r.db.Where("task_id = ? AND job_type = ?", taskID, jobType).First(&existing).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	var task model.VideoTask
	if err := r.db.First(&task, taskID).Error; err != nil {
		return err
	}
	if maxRetries <= 0 {
		maxRetries = task.MaxRetries
	}
	if maxRetries <= 0 {
		maxRetries = 3
	}
	return r.db.Create(&model.TaskJob{
		TaskID:     task.ID,
		UserID:     task.UserID,
		JobType:    jobType,
		Status:     task.Status,
		Stage:      stage,
		TraceID:    task.TraceID,
		RetryCount: 0,
		MaxRetries: maxRetries,
	}).Error
}

// BindRetryBudget assigns a durable budget once. Replays may bind the same ID,
// but cannot replace an existing budget with a different one.
func (r *TaskJobRepository) BindRetryBudget(taskID int64, jobType, budgetID string) (bool, error) {
	if taskID <= 0 || jobType == "" || budgetID == "" {
		return false, errors.New("invalid task retry budget binding")
	}
	result := r.db.Model(&model.TaskJob{}).
		Where("task_id = ? AND job_type = ? AND (retry_budget_id = '' OR retry_budget_id IS NULL)", taskID, jobType).
		Update("retry_budget_id", budgetID)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected > 0 {
		return true, nil
	}
	var job model.TaskJob
	if err := r.db.Where("task_id = ? AND job_type = ?", taskID, jobType).First(&job).Error; err != nil {
		return false, err
	}
	if job.RetryBudgetID != budgetID {
		return false, errors.New("task job already uses a different retry budget")
	}
	return false, nil
}
