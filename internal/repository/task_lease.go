package repository

import (
	"fmt"
	"time"

	"vid-lens/internal/model"

	"gorm.io/gorm/clause"
)

type TaskLeaseOutcome string

const (
	TaskLeaseAcquired TaskLeaseOutcome = "acquired"
	TaskLeaseBusy     TaskLeaseOutcome = "busy"
	TaskLeaseStale    TaskLeaseOutcome = "stale"
	TaskLeaseTerminal TaskLeaseOutcome = "terminal"
)

type TaskLeaseClaim struct {
	Outcome TaskLeaseOutcome
	Token   string
	Version int64
}

type TaskProcessingClaimRequest struct {
	TaskID       int64
	JobType      string
	Stage        string
	MessageToken string
	Now          time.Time
	LeaseUntil   time.Time
	NewToken     string
}

type TaskDispatchClaimRequest struct {
	TaskID          int64
	JobType         string
	Stage           string
	ExpectedVersion int64
	Now             time.Time
	LeaseUntil      time.Time
	Token           string
}

type TaskDispatchRestoreRequest struct {
	TaskID       int64
	JobType      string
	Stage        string
	Token        string
	ErrorMessage string
	NextRetryAt  time.Time
}

func (r *Repositories) ClaimTaskProcessing(req TaskProcessingClaimRequest) (TaskLeaseClaim, error) {
	if r == nil || r.Task == nil || r.TaskJob == nil {
		return TaskLeaseClaim{}, fmt.Errorf("任务仓储未初始化")
	}
	if req.TaskID <= 0 || req.JobType == "" || req.NewToken == "" {
		return TaskLeaseClaim{}, fmt.Errorf("处理 lease 参数不完整")
	}

	result := TaskLeaseClaim{Outcome: TaskLeaseBusy}
	err := r.Transaction(func(repos *Repositories) error {
		task, err := repos.Task.FindByID(req.TaskID)
		if err != nil {
			return err
		}
		job, err := repos.TaskJob.FindByTaskAndType(req.TaskID, req.JobType)
		if err != nil {
			return err
		}
		if task.Status == model.TaskStatusCompleted || task.Status == model.TaskStatusDead || (job != nil && job.Status == model.TaskStatusCompleted) {
			result = TaskLeaseClaim{Outcome: TaskLeaseTerminal}
			return nil
		}

		if req.MessageToken != "" {
			if task.ProcessingToken != req.MessageToken || task.LeaseKind != model.TaskLeaseKindDispatch || task.LeaseExpiresAt == nil || !task.LeaseExpiresAt.After(req.Now) {
				result = TaskLeaseClaim{Outcome: TaskLeaseStale}
				return nil
			}
			if job != nil && (job.ProcessingToken != req.MessageToken || job.LeaseKind != model.TaskLeaseKindDispatch || job.LeaseExpiresAt == nil || !job.LeaseExpiresAt.After(req.Now)) {
				result = TaskLeaseClaim{Outcome: TaskLeaseStale}
				return nil
			}
		} else {
			if task.Status == model.TaskStatusFailed && task.NextRetryAt != nil {
				result = TaskLeaseClaim{Outcome: TaskLeaseStale}
				return nil
			}
			if task.Status == model.TaskStatusFailed {
				result = TaskLeaseClaim{Outcome: TaskLeaseTerminal}
				return nil
			}
			if task.ProcessingToken != "" {
				if task.LeaseKind == model.TaskLeaseKindDispatch {
					result = TaskLeaseClaim{Outcome: TaskLeaseStale}
					return nil
				}
				if task.LeaseExpiresAt != nil && task.LeaseExpiresAt.After(req.Now) {
					result = TaskLeaseClaim{Outcome: TaskLeaseBusy}
					return nil
				}
			}
		}

		newVersion := task.LeaseVersion + 1
		updates := map[string]interface{}{
			"status":            model.TaskStatusRunning,
			"stage":             req.Stage,
			"last_job_type":     req.JobType,
			"processing_token":  req.NewToken,
			"lease_kind":        model.TaskLeaseKindProcessing,
			"lease_expires_at":  req.LeaseUntil,
			"lease_version":     newVersion,
			"next_retry_at":     nil,
			"error_msg":         "",
			"stage_started_at":  req.Now,
			"stage_finished_at": nil,
		}
		tx := repos.db.Model(&model.VideoTask{}).
			Where("id = ? AND lease_version = ?", task.ID, task.LeaseVersion).
			Updates(updates)
		if tx.Error != nil {
			return tx.Error
		}
		if tx.RowsAffected != 1 {
			result = TaskLeaseClaim{Outcome: TaskLeaseBusy}
			return nil
		}

		jobUpdates := map[string]interface{}{
			"user_id":          task.UserID,
			"status":           model.TaskStatusRunning,
			"stage":            req.Stage,
			"trace_id":         task.TraceID,
			"processing_token": req.NewToken,
			"lease_kind":       model.TaskLeaseKindProcessing,
			"lease_expires_at": req.LeaseUntil,
			"lease_version":    newVersion,
			"next_retry_at":    nil,
			"last_error_code":  "",
			"last_error_msg":   "",
			"started_at":       req.Now,
			"finished_at":      nil,
		}
		if job == nil {
			maxRetries := task.MaxRetries
			if maxRetries <= 0 {
				maxRetries = 3
			}
			job = &model.TaskJob{
				TaskID: task.ID, UserID: task.UserID, JobType: req.JobType,
				Status: model.TaskStatusRunning, Stage: req.Stage, TraceID: task.TraceID,
				RetryCount: 0, MaxRetries: maxRetries,
				ProcessingToken: req.NewToken, LeaseKind: model.TaskLeaseKindProcessing,
				LeaseExpiresAt: &req.LeaseUntil, LeaseVersion: newVersion, StartedAt: &req.Now,
			}
			if err := repos.db.Create(job).Error; err != nil {
				return err
			}
		} else {
			jobTx := repos.db.Model(&model.TaskJob{}).
				Where("id = ? AND lease_version = ?", job.ID, job.LeaseVersion).
				Updates(jobUpdates)
			if jobTx.Error != nil {
				return jobTx.Error
			}
			if jobTx.RowsAffected != 1 {
				return fmt.Errorf("子任务 processing lease CAS 失败")
			}
		}
		result = TaskLeaseClaim{Outcome: TaskLeaseAcquired, Token: req.NewToken, Version: newVersion}
		return nil
	})
	return result, err
}

func (r *Repositories) ClaimRetryDispatch(req TaskDispatchClaimRequest) (bool, error) {
	if r == nil || r.Task == nil || r.TaskJob == nil {
		return false, fmt.Errorf("任务仓储未初始化")
	}
	if req.TaskID <= 0 || req.JobType == "" || req.Token == "" {
		return false, fmt.Errorf("调度 lease 参数不完整")
	}

	claimed := false
	err := r.Transaction(func(repos *Repositories) error {
		task, err := repos.Task.FindByID(req.TaskID)
		if err != nil {
			return err
		}
		if task.LeaseVersion != req.ExpectedVersion || task.Status == model.TaskStatusCompleted || task.Status == model.TaskStatusDead {
			return nil
		}
		job, err := repos.TaskJob.FindByTaskAndType(task.ID, req.JobType)
		if err != nil {
			return err
		}
		if job == nil {
			maxRetries := task.MaxRetries
			if maxRetries <= 0 {
				maxRetries = 3
			}
			// Legacy rows may predate task_jobs. Recover them with a fresh stage
			// budget instead of inheriting VideoTask.RetryCount.
			job = &model.TaskJob{
				TaskID: task.ID, UserID: task.UserID, JobType: req.JobType,
				Status: task.Status, Stage: req.Stage, TraceID: task.TraceID,
				RetryCount: 0, MaxRetries: maxRetries, NextRetryAt: task.NextRetryAt,
				ProcessingToken: task.ProcessingToken, LeaseKind: task.LeaseKind,
				LeaseExpiresAt: task.LeaseExpiresAt, LeaseVersion: task.LeaseVersion,
			}
			if err := repos.db.Create(job).Error; err != nil {
				return err
			}
		}
		if job.Status == model.TaskStatusCompleted || job.RetryCount > job.MaxRetries {
			return nil
		}
		// video_tasks keeps the current-stage due time as a compatibility mirror;
		// the stage budget and child lifecycle remain authoritative on TaskJob.
		parentDue := task.Status == model.TaskStatusFailed && task.NextRetryAt != nil && !task.NextRetryAt.After(req.Now)
		jobDue := job.Status == model.TaskStatusFailed && job.NextRetryAt != nil && !job.NextRetryAt.After(req.Now)
		dueFailure := parentDue || jobDue
		expiredLease := job.ProcessingToken != "" && job.LeaseExpiresAt != nil && !job.LeaseExpiresAt.After(req.Now) && (job.Status == model.TaskStatusQueued || job.Status == model.TaskStatusRunning)
		if !dueFailure && !expiredLease {
			return nil
		}

		newVersion := task.LeaseVersion + 1
		taskTx := repos.db.Model(&model.VideoTask{}).
			Where("id = ? AND lease_version = ?", task.ID, req.ExpectedVersion).
			Updates(map[string]interface{}{
				"status":            model.TaskStatusQueued,
				"stage":             req.Stage,
				"last_job_type":     req.JobType,
				"processing_token":  req.Token,
				"lease_kind":        model.TaskLeaseKindDispatch,
				"lease_expires_at":  req.LeaseUntil,
				"lease_version":     newVersion,
				"next_retry_at":     nil,
				"error_msg":         "",
				"stage_started_at":  req.Now,
				"stage_finished_at": nil,
			})
		if taskTx.Error != nil {
			return taskTx.Error
		}
		if taskTx.RowsAffected != 1 {
			return nil
		}

		maxRetries := 3
		if job != nil && job.MaxRetries > 0 {
			maxRetries = job.MaxRetries
		} else if task.MaxRetries > 0 {
			maxRetries = task.MaxRetries
		}
		if job == nil {
			job = &model.TaskJob{
				TaskID: task.ID, UserID: task.UserID, JobType: req.JobType,
				Status: model.TaskStatusQueued, Stage: req.Stage, TraceID: task.TraceID,
				RetryCount: 0, MaxRetries: maxRetries,
				ProcessingToken: req.Token, LeaseKind: model.TaskLeaseKindDispatch,
				LeaseExpiresAt: &req.LeaseUntil, LeaseVersion: newVersion,
			}
			if err := repos.db.Create(job).Error; err != nil {
				return err
			}
		} else {
			jobTx := repos.db.Model(&model.TaskJob{}).
				Where("id = ? AND lease_version = ?", job.ID, job.LeaseVersion).
				Updates(map[string]interface{}{
					"status":           model.TaskStatusQueued,
					"stage":            req.Stage,
					"processing_token": req.Token,
					"lease_kind":       model.TaskLeaseKindDispatch,
					"lease_expires_at": req.LeaseUntil,
					"lease_version":    newVersion,
					"next_retry_at":    nil,
					"last_error_code":  "",
					"last_error_msg":   "",
					"finished_at":      nil,
				})
			if jobTx.Error != nil {
				return jobTx.Error
			}
			if jobTx.RowsAffected != 1 {
				return fmt.Errorf("子任务 dispatch lease CAS 失败")
			}
		}
		claimed = true
		return nil
	})
	return claimed, err
}

func (r *Repositories) RestoreRetryDispatch(req TaskDispatchRestoreRequest) (bool, error) {
	if r == nil || r.Task == nil || r.TaskJob == nil {
		return false, fmt.Errorf("任务仓储未初始化")
	}
	if req.TaskID <= 0 || req.JobType == "" || req.Token == "" {
		return false, fmt.Errorf("恢复 dispatch lease 参数不完整")
	}

	restored := false
	err := r.Transaction(func(repos *Repositories) error {
		task, err := repos.Task.FindByID(req.TaskID)
		if err != nil {
			return err
		}
		if task.ProcessingToken != req.Token || task.LeaseKind != model.TaskLeaseKindDispatch {
			return nil
		}
		job, err := repos.TaskJob.FindByTaskAndType(req.TaskID, req.JobType)
		if err != nil {
			return err
		}
		if job == nil {
			return fmt.Errorf("dispatch 子任务不存在")
		}
		if job.ProcessingToken != req.Token || job.LeaseKind != model.TaskLeaseKindDispatch {
			return nil
		}
		newVersion := task.LeaseVersion + 1
		now := time.Now()
		taskTx := repos.db.Model(&model.VideoTask{}).
			Where("id = ? AND processing_token = ? AND lease_kind = ? AND lease_version = ?", task.ID, req.Token, model.TaskLeaseKindDispatch, task.LeaseVersion).
			Updates(map[string]interface{}{
				"status":            model.TaskStatusFailed,
				"stage":             req.Stage,
				"error_msg":         req.ErrorMessage,
				"last_error_code":   "retry_enqueue_failed",
				"last_error_msg":    req.ErrorMessage,
				"next_retry_at":     req.NextRetryAt,
				"processing_token":  "",
				"lease_kind":        "",
				"lease_expires_at":  nil,
				"lease_version":     newVersion,
				"stage_finished_at": now,
			})
		if taskTx.Error != nil {
			return taskTx.Error
		}
		if taskTx.RowsAffected != 1 {
			return nil
		}
		jobTx := repos.db.Model(&model.TaskJob{}).
			Where("id = ? AND processing_token = ? AND lease_kind = ? AND lease_version = ?", job.ID, req.Token, model.TaskLeaseKindDispatch, job.LeaseVersion).
			Updates(map[string]interface{}{
				"status":           model.TaskStatusFailed,
				"stage":            req.Stage,
				"next_retry_at":    req.NextRetryAt,
				"last_error_code":  "retry_enqueue_failed",
				"last_error_msg":   req.ErrorMessage,
				"processing_token": "",
				"lease_kind":       "",
				"lease_expires_at": nil,
				"lease_version":    newVersion,
				"finished_at":      now,
			})
		if jobTx.Error != nil {
			return jobTx.Error
		}
		if jobTx.RowsAffected != 1 {
			return fmt.Errorf("子任务 dispatch 恢复 CAS 失败")
		}
		restored = true
		return nil
	})
	return restored, err
}

// TaskProcessingCompleteRequest completes the current processing owner using its lease token.
type TaskProcessingCompleteRequest struct {
	TaskID     int64
	JobType    string
	JobStage   string
	Token      string
	TaskStatus int8
	TaskStage  string
	TaskFields map[string]interface{}
	Now        time.Time
}

// TaskProcessingFailureRequest records a durable failure for the current processing owner.
type TaskProcessingFailureRequest struct {
	TaskID       int64
	JobType      string
	Stage        string
	Token        string
	Status       int8
	ErrorCode    string
	ErrorMessage string
	RetryCount   int
	MaxRetries   int
	NextRetryAt  *time.Time
	Now          time.Time
}

func (r *Repositories) CompleteTaskProcessing(req TaskProcessingCompleteRequest) (bool, error) {
	if r == nil || r.Task == nil || r.TaskJob == nil {
		return false, fmt.Errorf("任务仓储未初始化")
	}
	if req.TaskID <= 0 || req.JobType == "" || req.Token == "" {
		return false, fmt.Errorf("完成 processing lease 参数不完整")
	}
	completed := false
	err := r.Transaction(func(repos *Repositories) error {
		task, err := repos.Task.FindByID(req.TaskID)
		if err != nil {
			return err
		}
		job, err := repos.TaskJob.FindByTaskAndType(req.TaskID, req.JobType)
		if err != nil {
			return err
		}
		if job == nil {
			return fmt.Errorf("processing 子任务不存在")
		}
		now := req.Now
		if now.IsZero() {
			now = time.Now()
		}
		if !ownsProcessingLease(task, job, req.Token, now) {
			return nil
		}
		newVersion := task.LeaseVersion + 1
		taskUpdates := make(map[string]interface{}, len(req.TaskFields)+12)
		for key, value := range req.TaskFields {
			taskUpdates[key] = value
		}
		taskUpdates["status"] = req.TaskStatus
		taskUpdates["stage"] = req.TaskStage
		taskUpdates["next_retry_at"] = nil
		taskUpdates["last_error_code"] = ""
		taskUpdates["last_error_msg"] = ""
		taskUpdates["error_msg"] = ""
		taskUpdates["processing_token"] = ""
		taskUpdates["lease_kind"] = ""
		taskUpdates["lease_expires_at"] = nil
		taskUpdates["lease_version"] = newVersion
		/* protected lifecycle fields above intentionally override TaskFields */
		if req.TaskStatus == model.TaskStatusCompleted || req.TaskStatus == model.TaskStatusDead {
			taskUpdates["stage_finished_at"] = now
			taskUpdates["finished_at"] = now
		} else {
			taskUpdates["stage_started_at"] = now
			taskUpdates["stage_finished_at"] = nil
			taskUpdates["finished_at"] = nil
		}
		tx := repos.db.Model(&model.VideoTask{}).Where("id = ? AND processing_token = ? AND lease_kind = ? AND lease_version = ? AND lease_expires_at > ?", task.ID, req.Token, model.TaskLeaseKindProcessing, task.LeaseVersion, now).Updates(taskUpdates)
		if tx.Error != nil {
			return tx.Error
		}
		if tx.RowsAffected != 1 {
			return nil
		}
		jobTx := repos.db.Model(&model.TaskJob{}).Where("id = ? AND processing_token = ? AND lease_kind = ? AND lease_version = ? AND lease_expires_at > ?", job.ID, req.Token, model.TaskLeaseKindProcessing, job.LeaseVersion, now).Updates(map[string]interface{}{
			"status": model.TaskStatusCompleted, "stage": req.JobStage, "next_retry_at": nil,
			"last_error_code": "", "last_error_msg": "", "processing_token": "", "lease_kind": "", "lease_expires_at": nil,
			"lease_version": newVersion, "finished_at": now,
		})
		if jobTx.Error != nil {
			return jobTx.Error
		}
		if jobTx.RowsAffected != 1 {
			return fmt.Errorf("子任务完成 processing lease CAS 失败")
		}
		completed = true
		return nil
	})
	return completed, err
}

func (r *Repositories) FailTaskProcessing(req TaskProcessingFailureRequest) (bool, error) {
	if r == nil || r.Task == nil || r.TaskJob == nil {
		return false, fmt.Errorf("任务仓储未初始化")
	}
	if req.TaskID <= 0 || req.JobType == "" || req.Token == "" {
		return false, fmt.Errorf("失败 processing lease 参数不完整")
	}
	failed := false
	err := r.Transaction(func(repos *Repositories) error {
		task, err := repos.Task.FindByID(req.TaskID)
		if err != nil {
			return err
		}
		job, err := repos.TaskJob.FindByTaskAndType(req.TaskID, req.JobType)
		if err != nil {
			return err
		}
		if job == nil {
			return fmt.Errorf("processing 子任务不存在")
		}
		now := req.Now
		if now.IsZero() {
			now = time.Now()
		}
		if !ownsProcessingLease(task, job, req.Token, now) {
			return nil
		}
		newVersion := task.LeaseVersion + 1
		taskTx := repos.db.Model(&model.VideoTask{}).Where("id = ? AND processing_token = ? AND lease_kind = ? AND lease_version = ? AND lease_expires_at > ?", task.ID, req.Token, model.TaskLeaseKindProcessing, task.LeaseVersion, now).Updates(map[string]interface{}{
			"status": req.Status, "stage": req.Stage, "last_job_type": req.JobType,
			"retry_count": req.RetryCount, "max_retries": req.MaxRetries, "next_retry_at": req.NextRetryAt,
			"last_error_code": req.ErrorCode, "last_error_msg": req.ErrorMessage, "error_msg": req.ErrorMessage,
			"processing_token": "", "lease_kind": "", "lease_expires_at": nil, "lease_version": newVersion,
			"stage_finished_at": now,
		})
		if taskTx.Error != nil {
			return taskTx.Error
		}
		if taskTx.RowsAffected != 1 {
			return nil
		}
		jobTx := repos.db.Model(&model.TaskJob{}).Where("id = ? AND processing_token = ? AND lease_kind = ? AND lease_version = ? AND lease_expires_at > ?", job.ID, req.Token, model.TaskLeaseKindProcessing, job.LeaseVersion, now).Updates(map[string]interface{}{
			"status": req.Status, "stage": req.Stage, "retry_count": req.RetryCount, "max_retries": req.MaxRetries,
			"next_retry_at": req.NextRetryAt, "last_error_code": req.ErrorCode, "last_error_msg": req.ErrorMessage,
			"processing_token": "", "lease_kind": "", "lease_expires_at": nil, "lease_version": newVersion, "finished_at": now,
		})
		if jobTx.Error != nil {
			return jobTx.Error
		}
		if jobTx.RowsAffected != 1 {
			return fmt.Errorf("子任务失败 processing lease CAS 失败")
		}
		failed = true
		return nil
	})
	return failed, err
}

// TaskProcessingLeaseRequest identifies the current owner of one processing stage.
type TaskProcessingLeaseRequest struct {
	TaskID     int64
	JobType    string
	Token      string
	Now        time.Time
	LeaseUntil time.Time
}

// TaskProcessingHandoffFailureRequest atomically completes the current stage and
// makes the next stage retryable when its Kafka handoff cannot be published.
type TaskProcessingHandoffFailureRequest struct {
	TaskID         int64
	CurrentJobType string
	CurrentStage   string
	NextJobType    string
	NextStage      string
	Token          string
	Status         int8
	ErrorCode      string
	ErrorMessage   string
	RetryCount     int
	MaxRetries     int
	NextRetryAt    *time.Time
	Now            time.Time
}

func ownsProcessingLease(task *model.VideoTask, job *model.TaskJob, token string, now time.Time) bool {
	return task != nil && job != nil && token != "" &&
		task.ProcessingToken == token && task.LeaseKind == model.TaskLeaseKindProcessing &&
		task.LeaseExpiresAt != nil && task.LeaseExpiresAt.After(now) &&
		job.ProcessingToken == token && job.LeaseKind == model.TaskLeaseKindProcessing &&
		job.LeaseExpiresAt != nil && job.LeaseExpiresAt.After(now)
}

// OwnsTaskProcessing checks both the parent compatibility row and the stage row.
// Expired leases are rejected even if no replacement worker has claimed them yet.
func (r *Repositories) OwnsTaskProcessing(req TaskProcessingLeaseRequest) (bool, error) {
	if r == nil || r.Task == nil || r.TaskJob == nil {
		return false, fmt.Errorf("任务仓储未初始化")
	}
	if req.TaskID <= 0 || req.JobType == "" || req.Token == "" {
		return false, fmt.Errorf("processing lease 参数不完整")
	}
	if req.Now.IsZero() {
		req.Now = time.Now()
	}
	var count int64
	err := r.db.Table("video_tasks AS t").
		Joins("JOIN task_jobs AS j ON j.task_id = t.id AND j.job_type = ?", req.JobType).
		Where("t.id = ? AND t.processing_token = ? AND t.lease_kind = ? AND t.lease_expires_at > ?", req.TaskID, req.Token, model.TaskLeaseKindProcessing, req.Now).
		Where("j.processing_token = ? AND j.lease_kind = ? AND j.lease_expires_at > ?", req.Token, model.TaskLeaseKindProcessing, req.Now).
		Count(&count).Error
	return count == 1, err
}

// RenewTaskProcessing extends task and TaskJob leases in one transaction. A
// partial renewal is rolled back so the worker fails closed on inconsistent rows.
func (r *Repositories) RenewTaskProcessing(req TaskProcessingLeaseRequest) (bool, error) {
	if r == nil || r.Task == nil || r.TaskJob == nil {
		return false, fmt.Errorf("任务仓储未初始化")
	}
	if req.TaskID <= 0 || req.JobType == "" || req.Token == "" || req.LeaseUntil.IsZero() {
		return false, fmt.Errorf("processing lease 续租参数不完整")
	}
	if req.Now.IsZero() {
		req.Now = time.Now()
	}
	renewed := false
	err := r.Transaction(func(repos *Repositories) error {
		taskTx := repos.db.Model(&model.VideoTask{}).
			Where("id = ? AND processing_token = ? AND lease_kind = ? AND lease_expires_at > ?", req.TaskID, req.Token, model.TaskLeaseKindProcessing, req.Now).
			Update("lease_expires_at", req.LeaseUntil)
		if taskTx.Error != nil {
			return taskTx.Error
		}
		if taskTx.RowsAffected != 1 {
			return nil
		}
		jobTx := repos.db.Model(&model.TaskJob{}).
			Where("task_id = ? AND job_type = ? AND processing_token = ? AND lease_kind = ? AND lease_expires_at > ?", req.TaskID, req.JobType, req.Token, model.TaskLeaseKindProcessing, req.Now).
			Update("lease_expires_at", req.LeaseUntil)
		if jobTx.Error != nil {
			return jobTx.Error
		}
		if jobTx.RowsAffected != 1 {
			return fmt.Errorf("子任务 processing lease 续租 CAS 失败")
		}
		renewed = true
		return nil
	})
	return renewed, err
}

// RunWithTaskProcessingLease fences a database side effect with row locks. It
// does not make remote provider/object-storage calls exactly-once; those remain
// at-least-once and must use their own idempotency keys or persisted results.
func (r *Repositories) RunWithTaskProcessingLease(req TaskProcessingLeaseRequest, fn func(*Repositories) error) (bool, error) {
	if r == nil || r.Task == nil || r.TaskJob == nil {
		return false, fmt.Errorf("任务仓储未初始化")
	}
	if req.TaskID <= 0 || req.JobType == "" || req.Token == "" || fn == nil {
		return false, fmt.Errorf("processing lease 副作用参数不完整")
	}
	if req.Now.IsZero() {
		req.Now = time.Now()
	}
	owned := false
	err := r.Transaction(func(repos *Repositories) error {
		var task model.VideoTask
		if err := repos.db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, req.TaskID).Error; err != nil {
			return err
		}
		var job model.TaskJob
		if err := repos.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("task_id = ? AND job_type = ?", req.TaskID, req.JobType).First(&job).Error; err != nil {
			return err
		}
		if !ownsProcessingLease(&task, &job, req.Token, req.Now) {
			return nil
		}
		if err := fn(repos); err != nil {
			return err
		}
		owned = true
		return nil
	})
	return owned, err
}

func (r *Repositories) FailTaskProcessingHandoff(req TaskProcessingHandoffFailureRequest) (bool, error) {
	if r == nil || r.Task == nil || r.TaskJob == nil {
		return false, fmt.Errorf("任务仓储未初始化")
	}
	if req.TaskID <= 0 || req.CurrentJobType == "" || req.NextJobType == "" || req.Token == "" {
		return false, fmt.Errorf("processing handoff 参数不完整")
	}
	if req.Now.IsZero() {
		req.Now = time.Now()
	}
	if req.MaxRetries <= 0 {
		req.MaxRetries = 3
	}
	updated := false
	err := r.Transaction(func(repos *Repositories) error {
		task, err := repos.Task.FindByID(req.TaskID)
		if err != nil {
			return err
		}
		currentJob, err := repos.TaskJob.FindByTaskAndType(req.TaskID, req.CurrentJobType)
		if err != nil {
			return err
		}
		if !ownsProcessingLease(task, currentJob, req.Token, req.Now) {
			return nil
		}

		newVersion := task.LeaseVersion + 1
		taskTx := repos.db.Model(&model.VideoTask{}).
			Where("id = ? AND processing_token = ? AND lease_kind = ? AND lease_version = ? AND lease_expires_at > ?", task.ID, req.Token, model.TaskLeaseKindProcessing, task.LeaseVersion, req.Now).
			Updates(map[string]interface{}{
				"status": req.Status, "stage": req.NextStage, "last_job_type": req.NextJobType,
				"retry_count": req.RetryCount, "max_retries": req.MaxRetries, "next_retry_at": req.NextRetryAt,
				"last_error_code": req.ErrorCode, "last_error_msg": req.ErrorMessage, "error_msg": req.ErrorMessage,
				"processing_token": "", "lease_kind": "", "lease_expires_at": nil, "lease_version": newVersion,
				"stage_finished_at": req.Now,
			})
		if taskTx.Error != nil {
			return taskTx.Error
		}
		if taskTx.RowsAffected != 1 {
			return nil
		}

		currentTx := repos.db.Model(&model.TaskJob{}).
			Where("id = ? AND processing_token = ? AND lease_kind = ? AND lease_version = ? AND lease_expires_at > ?", currentJob.ID, req.Token, model.TaskLeaseKindProcessing, currentJob.LeaseVersion, req.Now).
			Updates(map[string]interface{}{
				"status": model.TaskStatusCompleted, "stage": req.CurrentStage, "next_retry_at": nil,
				"last_error_code": "", "last_error_msg": "", "processing_token": "", "lease_kind": "", "lease_expires_at": nil,
				"lease_version": newVersion, "finished_at": req.Now,
			})
		if currentTx.Error != nil {
			return currentTx.Error
		}
		if currentTx.RowsAffected != 1 {
			return fmt.Errorf("当前子任务 handoff CAS 失败")
		}

		nextJob, err := repos.TaskJob.FindByTaskAndType(req.TaskID, req.NextJobType)
		if err != nil {
			return err
		}
		nextUpdates := map[string]interface{}{
			"user_id": task.UserID, "status": req.Status, "stage": req.NextStage, "trace_id": task.TraceID,
			"retry_count": req.RetryCount, "max_retries": req.MaxRetries, "next_retry_at": req.NextRetryAt,
			"last_error_code": req.ErrorCode, "last_error_msg": req.ErrorMessage,
			"processing_token": "", "lease_kind": "", "lease_expires_at": nil, "finished_at": req.Now,
		}
		if nextJob == nil {
			nextJob = &model.TaskJob{TaskID: task.ID, UserID: task.UserID, JobType: req.NextJobType}
			if err := repos.db.Create(nextJob).Error; err != nil {
				return err
			}
		}
		if err := repos.db.Model(&model.TaskJob{}).Where("id = ?", nextJob.ID).Updates(nextUpdates).Error; err != nil {
			return err
		}
		updated = true
		return nil
	})
	return updated, err
}
