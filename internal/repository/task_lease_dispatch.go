package repository

import (
	"fmt"
	"time"

	"vid-lens/internal/model"
)

// RetryScheduler 获取和恢复 dispatch lease；Kafka 投递失败时必须可恢复。
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
		job, err := repos.findOrCreateRetryDispatchJob(task, req)
		if err != nil {
			return err
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
		claimed = true
		return nil
	})
	return claimed, err
}

// findOrCreateRetryDispatchJob bridges rows created before task_jobs existed.
// It always returns a persisted child row, so the claim path below has one CAS
// update instead of separate legacy and current branches.
func (r *Repositories) findOrCreateRetryDispatchJob(task *model.VideoTask, req TaskDispatchClaimRequest) (*model.TaskJob, error) {
	job, err := r.TaskJob.FindByTaskAndType(task.ID, req.JobType)
	if err != nil || job != nil {
		return job, err
	}

	maxRetries := task.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	job = &model.TaskJob{
		TaskID: task.ID, UserID: task.UserID, JobType: req.JobType,
		Status: task.Status, Stage: req.Stage, TraceID: task.TraceID,
		RetryCount: 0, MaxRetries: maxRetries, NextRetryAt: task.NextRetryAt,
		ProcessingToken: task.ProcessingToken, LeaseKind: task.LeaseKind,
		LeaseExpiresAt: task.LeaseExpiresAt, LeaseVersion: task.LeaseVersion,
	}
	if err := r.db.Create(job).Error; err != nil {
		return nil, err
	}
	return job, nil
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
