package repository

import (
	"fmt"

	"vid-lens/internal/model"
)

// 消费者获取 processing lease：父任务和子任务通过 CAS 一起推进。
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
		// started_at is the overall task start and is written once, when a
		// worker first owns processing. stage_started_at changes per stage.
		if task.StartedAt == nil {
			updates["started_at"] = req.Now
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
