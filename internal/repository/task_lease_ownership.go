package repository

import (
	"fmt"
	"time"

	"vid-lens/internal/model"

	"gorm.io/gorm/clause"
)

// lease 所有权检查、续租、带行锁副作用和跨阶段 handoff。
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
