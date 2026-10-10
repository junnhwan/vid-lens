package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"vid-lens/internal/model"
)

var ErrSummaryAvailable = errors.New("任务已完成，可直接查看结果")

// Summary work reads a durable transcript snapshot and only owns task_jobs.
// Lock the parent first to serialize dispatch with deletion/transcription, but
// never replace its processing token or its visual/index lifecycle.
func SummaryJobActive(job *model.TaskJob) bool {
	return job != nil && (job.Status == model.TaskStatusQueued || job.Status == model.TaskStatusRunning || (job.Status == model.TaskStatusFailed && job.NextRetryAt != nil))
}

func SummarySourceReady(task *model.VideoTask) bool {
	if task.Status != model.TaskStatusQueued && task.Status != model.TaskStatusRunning {
		return !(task.LastJobType == model.TaskJobTypeAnalyze && task.NextRetryAt != nil)
	}
	if task.LastJobType == model.TaskJobTypeAnalyze || task.Stage == model.TaskStageDownloading {
		return false
	}
	if task.LastJobType == model.TaskJobTypeTranscribe {
		return task.Status == model.TaskStatusRunning && (task.Stage == model.TaskStageVisual || task.Stage == model.TaskStageIndexing)
	}
	return task.Stage == model.TaskStageVisual || task.Stage == model.TaskStageIndexing
}

func (r *Repositories) prepareSummaryDispatch(req InitialTaskDispatchRequest) (InitialTaskDispatch, error) {
	var prepared InitialTaskDispatch
	err := r.Transaction(func(tx *Repositories) error {
		task, err := tx.Task.FindByIDForUpdate(req.Task.ID)
		if err != nil {
			return err
		}
		if task.UserID != req.Task.UserID || !SummarySourceReady(task) {
			return ErrInitialTaskDispatchConflict
		}
		job, err := tx.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeSummary)
		if err != nil {
			return err
		}
		if SummaryJobActive(job) {
			return fmt.Errorf("摘要正在生成或等待自动重试，请勿重复提交")
		}
		transcription, err := tx.Transcription.FindByTaskID(task.ID)
		if err != nil {
			return err
		}
		if transcription == nil && task.FileMD5 != "" && LegacyResultReuseAllowed(task) {
			transcription, err = tx.Transcription.FindByMD5(task.FileMD5)
		}
		if err != nil {
			return err
		}
		if transcription == nil || strings.TrimSpace(transcription.Content) == "" {
			return fmt.Errorf("请先完成转写")
		}
		if !LegacyResultReuseAllowed(task) {
			source, readErr := tx.TextSource.Active(context.Background(), task.UserID, task.ID)
			if readErr != nil {
				return readErr
			}
			if source == nil || source.ID != transcription.SourceID || source.SourceDigest != transcription.SourceDigest {
				return fmt.Errorf("完整文字来源尚未发布")
			}
		}
		summary, err := tx.Summary.FindByTaskID(task.ID)
		if err == nil && summary == nil && LegacyResultReuseAllowed(task) {
			summary, err = tx.Summary.FindByMD5(task.FileMD5)
		}
		if err != nil {
			return err
		}
		if summary != nil && !req.SummaryForce {
			return ErrSummaryAvailable
		}
		if req.SummaryForce && LegacyResultReuseAllowed(task) {
			if err := tx.Summary.DeleteByTaskID(task.ID); err != nil {
				return err
			}
		}
		if tx.SummaryPart != nil && !req.PreserveSummaryCheckpoints {
			if err := tx.SummaryPart.DeleteByTaskID(task.ID); err != nil {
				return err
			}
		}
		var chunks []model.VideoTranscriptionChunk
		if task.ActiveTextSourceID == "" {
			chunks, err = tx.TranscriptionChunk.ListByTaskID(transcription.TaskID)
		}
		if err != nil {
			return err
		}
		chunkJSON, err := json.Marshal(chunks)
		if err != nil {
			return err
		}
		if job == nil {
			job = &model.TaskJob{TaskID: task.ID, UserID: task.UserID, JobType: model.TaskJobTypeSummary}
			if err := tx.db.Create(job).Error; err != nil {
				return err
			}
		}
		maxRetries := task.MaxRetries
		if maxRetries <= 0 {
			maxRetries = 3
		}
		updates := map[string]interface{}{
			"status": model.TaskStatusQueued, "stage": model.TaskStageSummarizing, "trace_id": task.TraceID,
			"retry_count": 0, "max_retries": maxRetries, "retry_budget_id": "", "retry_budget_generation": job.RetryBudgetGeneration + 1,
			"next_retry_at": nil, "last_error_code": "", "last_error_msg": "", "started_at": nil, "finished_at": nil,
			"processing_token": req.Token, "lease_kind": model.TaskLeaseKindDispatch, "lease_expires_at": req.LeaseUntil,
			"lease_version": job.LeaseVersion + 1, "input_text": transcription.Content, "input_chunks_json": string(chunkJSON),
			"generation_id": "", "input_source_id": "", "input_snapshot_json": "",
		}
		if err := tx.db.Model(job).Updates(updates).Error; err != nil {
			return err
		}
		budgetID, err := tx.ensureTaskJobRetryBudget(task.ID, model.TaskJobTypeSummary, req.Now)
		if err != nil {
			return err
		}
		prepared = InitialTaskDispatch{Task: *task, RetryBudgetID: budgetID, Token: req.Token}
		return nil
	})
	return prepared, err
}

func (r *Repositories) withSummaryJob(taskID int64, fn func(*Repositories, *model.TaskJob) error) error {
	return r.Transaction(func(tx *Repositories) error {
		if _, err := tx.Task.FindByIDForUpdate(taskID); err != nil {
			return err
		}
		var job model.TaskJob
		if err := tx.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("task_id = ? AND job_type = ?", taskID, model.TaskJobTypeSummary).First(&job).Error; err != nil {
			return err
		}
		return fn(tx, &job)
	})
}

func ownsSummaryJob(job *model.TaskJob, token, kind string, now time.Time) bool {
	return token != "" && job.ProcessingToken == token && job.LeaseKind == kind && job.LeaseExpiresAt != nil && job.LeaseExpiresAt.After(now)
}

func (r *Repositories) claimSummaryProcessing(req TaskProcessingClaimRequest) (TaskLeaseClaim, error) {
	result := TaskLeaseClaim{Outcome: TaskLeaseStale}
	err := r.withSummaryJob(req.TaskID, func(tx *Repositories, job *model.TaskJob) error {
		if job.Status == model.TaskStatusCompleted || job.Status == model.TaskStatusDead {
			result.Outcome = TaskLeaseTerminal
			return nil
		}
		if req.MessageToken != "" {
			if !ownsSummaryJob(job, req.MessageToken, model.TaskLeaseKindDispatch, req.Now) {
				return nil
			}
		} else {
			// Every new summary dispatch carries a token. A tokenless delivery
			// cannot resurrect a failed job or steal a replacement worker's work.
			return nil
		}
		updates := map[string]interface{}{"status": model.TaskStatusRunning, "stage": model.TaskStageSummarizing, "processing_token": req.NewToken, "lease_kind": model.TaskLeaseKindProcessing, "lease_expires_at": req.LeaseUntil, "lease_version": job.LeaseVersion + 1, "started_at": req.Now, "finished_at": nil, "next_retry_at": nil}
		if err := tx.db.Model(job).Updates(updates).Error; err != nil {
			return err
		}
		result = TaskLeaseClaim{Outcome: TaskLeaseAcquired, Token: req.NewToken, Version: job.LeaseVersion}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return TaskLeaseClaim{Outcome: TaskLeaseStale}, nil
	}
	return result, err
}

func (r *Repositories) runWithSummaryLease(req TaskProcessingLeaseRequest, fn func(*Repositories, *model.TaskJob) error) (bool, error) {
	if req.Now.IsZero() {
		req.Now = time.Now()
	}
	owned := false
	err := r.withSummaryJob(req.TaskID, func(tx *Repositories, job *model.TaskJob) error {
		if job.Status != model.TaskStatusRunning || !ownsSummaryJob(job, req.Token, model.TaskLeaseKindProcessing, req.Now) {
			return nil
		}
		if err := fn(tx, job); err != nil {
			return err
		}
		owned = true
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return owned, err
}

func (r *Repositories) finishSummaryProcessing(req TaskProcessingFailureRequest) (bool, error) {
	return r.runWithSummaryLease(TaskProcessingLeaseRequest{TaskID: req.TaskID, Token: req.Token, Now: req.Now}, func(tx *Repositories, job *model.TaskJob) error {
		if err := tx.recordSummaryGenerationQueueFailure(job, req); err != nil {
			return err
		}
		return tx.db.Model(job).Updates(map[string]interface{}{"status": req.Status, "stage": model.TaskStageSummarizing, "retry_count": req.RetryCount, "max_retries": req.MaxRetries, "next_retry_at": req.NextRetryAt, "last_error_code": req.ErrorCode, "last_error_msg": req.ErrorMessage, "processing_token": "", "lease_kind": "", "lease_expires_at": nil, "lease_version": job.LeaseVersion + 1, "finished_at": req.Now}).Error
	})
}

func (r *Repositories) claimSummaryRetry(req TaskDispatchClaimRequest) (bool, error) {
	claimed := false
	err := r.withSummaryJob(req.TaskID, func(tx *Repositories, job *model.TaskJob) error {
		if job.LeaseVersion != req.ExpectedVersion || job.Status == model.TaskStatusCompleted || job.Status == model.TaskStatusDead || job.RetryCount > job.MaxRetries {
			return nil
		}
		due := job.Status == model.TaskStatusFailed && job.NextRetryAt != nil && !job.NextRetryAt.After(req.Now)
		expired := (job.Status == model.TaskStatusQueued || job.Status == model.TaskStatusRunning) && job.LeaseExpiresAt != nil && !job.LeaseExpiresAt.After(req.Now) && (job.NextRetryAt == nil || !job.NextRetryAt.After(req.Now))
		if !due && !expired {
			return nil
		}
		var next *time.Time
		if !due && req.RedispatchBackoff > 0 {
			v := req.Now.Add(req.RedispatchBackoff)
			next = &v
		}
		err := tx.db.Model(job).Updates(map[string]interface{}{"status": model.TaskStatusQueued, "processing_token": req.Token, "lease_kind": model.TaskLeaseKindDispatch, "lease_expires_at": req.LeaseUntil, "lease_version": job.LeaseVersion + 1, "next_retry_at": next, "last_error_code": "", "last_error_msg": "", "finished_at": nil}).Error
		claimed = err == nil
		return err
	})
	return claimed, err
}

func (r *Repositories) restoreSummaryDispatch(req TaskDispatchRestoreRequest, dead bool) (bool, error) {
	restored := false
	err := r.withSummaryJob(req.TaskID, func(tx *Repositories, job *model.TaskJob) error {
		if job.ProcessingToken != req.Token || job.LeaseKind != model.TaskLeaseKindDispatch {
			return nil
		}
		status, code := int8(model.TaskStatusFailed), "retry_enqueue_failed"
		var next *time.Time = &req.NextRetryAt
		if dead {
			status, code, next = model.TaskStatusDead, "retry_budget_exhausted", nil
			if err := tx.recordSummaryGenerationQueueFailure(job, TaskProcessingFailureRequest{Status: status, Now: time.Now()}); err != nil {
				return err
			}
		}
		err := tx.db.Model(job).Updates(map[string]interface{}{"status": status, "next_retry_at": next, "last_error_code": code, "last_error_msg": req.ErrorMessage, "processing_token": "", "lease_kind": "", "lease_expires_at": nil, "lease_version": job.LeaseVersion + 1, "finished_at": time.Now()}).Error
		restored = err == nil
		return err
	})
	return restored, err
}

// Independent jobs must remain discoverable even after the video is completed.
func (r *TaskJobRepository) DueSummaryTasks(now time.Time, limit int) ([]model.VideoTask, error) {
	var jobs []model.TaskJob
	err := r.db.Table("task_jobs AS j").Select("j.*").Joins("JOIN video_tasks t ON t.id=j.task_id AND t.deleted_at IS NULL").Where("j.job_type = ? AND j.retry_count <= j.max_retries", model.TaskJobTypeSummary).
		Where("(j.status = ? AND j.next_retry_at <= ?) OR (j.status IN ? AND j.lease_expires_at <= ? AND (j.next_retry_at IS NULL OR j.next_retry_at <= ?))", model.TaskStatusFailed, now, []int8{model.TaskStatusQueued, model.TaskStatusRunning}, now, now).Order("j.updated_at ASC").Limit(limit).Find(&jobs).Error
	if err != nil {
		return nil, err
	}
	tasks := make([]model.VideoTask, 0, len(jobs))
	for _, job := range jobs {
		var task model.VideoTask
		if err := r.db.First(&task, job.TaskID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, err
		}
		task.LastJobType, task.Stage, task.LeaseVersion, task.RetryCount = model.TaskJobTypeSummary, job.Stage, job.LeaseVersion, job.RetryCount
		tasks = append(tasks, task)
	}
	return tasks, nil
}
