package repository

import (
	"fmt"
	"time"

	"vid-lens/internal/model"
)

// processing 任务的完成与失败状态落库。
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
