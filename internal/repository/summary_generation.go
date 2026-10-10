package repository

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
	"vid-lens/internal/processing"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/summarydoc"
)

// SummaryGenerationExecutionStore keeps the journal's chat API unchanged and
// binds its generation adapter to one owner, task, and generation.
type SummaryGenerationExecutionStore struct {
	*AgentExecutionRepository
	repos         *Repositories
	owner, taskID int64
	generationID  string
}

func NewSummaryGenerationExecutionStore(repos *Repositories, owner, taskID int64, generationID string) *SummaryGenerationExecutionStore {
	return &SummaryGenerationExecutionStore{repos.AgentExecution, repos, owner, taskID, generationID}
}
func (s *SummaryGenerationExecutionStore) CreateRun(context.Context, *model.AgentRun) (bool, error) {
	return false, artifact.Err("generation_requires_job_lease", 409)
}
func (s *SummaryGenerationExecutionStore) GetRun(ctx context.Context, owner int64, id string) (*model.AgentRun, error) {
	if owner != s.owner || id != s.generationID {
		return nil, artifact.Err("not_found", 404)
	}
	var run model.AgentRun
	err := s.repos.db.WithContext(ctx).Where("agent_runs.id = ? AND agent_runs.user_id = ? AND agent_runs.task_id = ? AND agent_runs.subject_kind = ? AND agent_runs.subject_id = ?", id, owner, s.taskID, model.AgentRunSubjectSummaryGeneration, id).Where("EXISTS (SELECT 1 FROM video_tasks t WHERE t.id = agent_runs.task_id AND t.user_id = agent_runs.user_id AND t.deleted_at IS NULL)").First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &run, err
}
func (s *SummaryGenerationExecutionStore) GetExecution(ctx context.Context, owner int64, id string) (*AgentExecutionRecords, error) {
	run, err := s.GetRun(ctx, owner, id)
	if err != nil || run == nil {
		return nil, err
	}
	return s.AgentExecutionRepository.GetExecution(ctx, owner, id)
}
func (s *SummaryGenerationExecutionStore) requireRun(ctx context.Context, owner int64, id string) error {
	run, err := s.GetRun(ctx, owner, id)
	if err != nil {
		return err
	}
	if run == nil {
		return artifact.Err("not_found", 404)
	}
	return nil
}
func (s *SummaryGenerationExecutionStore) ClaimStep(ctx context.Context, req AgentStepClaimRequest) (AgentStepClaim, error) {
	if err := s.requireRun(ctx, req.UserID, req.RunID); err != nil {
		return AgentStepClaim{}, err
	}
	return s.AgentExecutionRepository.ClaimStep(ctx, req)
}
func (s *SummaryGenerationExecutionStore) CompleteStep(ctx context.Context, req AgentStepCompletion) (bool, error) {
	if err := s.requireRun(ctx, req.UserID, req.RunID); err != nil {
		return false, err
	}
	return s.AgentExecutionRepository.CompleteStep(ctx, req)
}
func (s *SummaryGenerationExecutionStore) FailStep(ctx context.Context, req AgentStepFailure) (bool, error) {
	if err := s.requireRun(ctx, req.UserID, req.RunID); err != nil {
		return false, err
	}
	return s.AgentExecutionRepository.FailStep(ctx, req)
}
func (s *SummaryGenerationExecutionStore) MarkFinalEvidenceRefs(ctx context.Context, owner int64, id string, refs []string) error {
	if err := s.requireRun(ctx, owner, id); err != nil {
		return err
	}
	return s.AgentExecutionRepository.MarkFinalEvidenceRefs(ctx, owner, id, refs)
}
func (s *SummaryGenerationExecutionStore) MarkRunTerminal(ctx context.Context, req AgentRunTerminalUpdate) (bool, error) {
	if err := s.requireRun(ctx, req.UserID, req.RunID); err != nil {
		return false, err
	}
	// Canonical generations terminalize only with the owning queue/source CAS;
	// the unleased chat journal API must not close a newer worker's run.
	return false, artifact.Err("generation_requires_job_lease", 409)
}

type SummaryGenerationLease struct {
	UserID, TaskID                                   int64
	GenerationID, SourceID, SourceDigest, LeaseToken string
}

func (r *Repositories) WithSummaryGenerationLease(ctx context.Context, req SummaryGenerationLease, fn func(*Repositories) error) error {
	owned, err := r.RunWithTaskProcessingLease(TaskProcessingLeaseRequest{TaskID: req.TaskID, JobType: model.TaskJobTypeSummary, Token: req.LeaseToken, Now: time.Now()}, func(tx *Repositories) error {
		if _, err := tx.TextSource.LockSource(ctx, req.UserID, req.TaskID, req.SourceID, req.SourceDigest); err != nil {
			return err
		}
		job, err := tx.TaskJob.FindByTaskAndType(req.TaskID, model.TaskJobTypeSummary)
		if err != nil {
			return err
		}
		if job == nil || job.UserID != req.UserID || job.GenerationID != req.GenerationID || job.InputSourceID != req.SourceID {
			return artifact.Err("generation_stale", 409)
		}
		var frozen processing.GenerationSnapshot
		if err := json.Unmarshal([]byte(job.InputSnapshotJSON), &frozen); err != nil || frozen.SourceID != req.SourceID || frozen.SourceDigest != req.SourceDigest || frozen.Intent.GenerationID != req.GenerationID {
			return artifact.Err("generation_stale", 409)
		}
		var run model.AgentRun
		runErr := tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND user_id=? AND task_id=? AND subject_kind=? AND subject_id=?", req.GenerationID, req.UserID, req.TaskID, model.AgentRunSubjectSummaryGeneration, req.GenerationID).First(&run).Error
		if runErr != nil && !errors.Is(runErr, gorm.ErrRecordNotFound) {
			return runErr
		}
		if runErr == nil && (run.CancelRequestedAt != nil || run.Status == model.AgentRunStatusCancelled) {
			return artifact.Err("generation_cancelled", 409)
		}
		return fn(tx)
	})
	if err != nil {
		return err
	}
	if !owned {
		return artifact.Err("generation_stale", 409)
	}
	return nil
}
func (r *Repositories) StartSummaryGeneration(ctx context.Context, req SummaryGenerationLease, run *model.AgentRun) (*model.AgentRun, error) {
	var saved *model.AgentRun
	err := r.WithSummaryGenerationLease(ctx, req, func(tx *Repositories) error {
		if run.ID != req.GenerationID || run.UserID != req.UserID || run.TaskID != req.TaskID || run.SubjectKind != model.AgentRunSubjectSummaryGeneration || run.SubjectID != req.GenerationID || run.SessionID != 0 || run.RecipeVersion == "" {
			return artifact.Err("invalid_generation_run", 400)
		}
		now := time.Now().UTC()
		run.CreatedAt = now
		run.UpdatedAt = now
		run.ExecutionStartedAt = &now
		run.Version = 1
		insert := tx.db.Clauses(clause.OnConflict{DoNothing: true}).Create(run)
		if insert.Error != nil {
			return insert.Error
		}
		store := NewSummaryGenerationExecutionStore(tx, req.UserID, req.TaskID, req.GenerationID)
		prior, err := store.GetRun(ctx, req.UserID, req.GenerationID)
		if err != nil {
			return err
		}
		if prior == nil || prior.ProfileSnapshot != run.ProfileSnapshot || prior.PolicySnapshot != run.PolicySnapshot || prior.BudgetSnapshot != run.BudgetSnapshot || prior.RecipeVersion != run.RecipeVersion {
			return artifact.Err("generation_checkpoint_changed", 409)
		}
		if insert.RowsAffected == 1 {
			title := "开始整理视频摘要"
			if run.Stage == "visual_retry" {
				title = "开始补充摘要画面"
			}
			if err = appendEvent(tx.db, prior, "run.started", map[string]any{"status": "running", "stage": run.Stage, "title": title}); err != nil {
				return err
			}
		}
		saved = prior
		return nil
	})
	return saved, err
}
func (r *Repositories) AppendSummaryGenerationEvent(ctx context.Context, req SummaryGenerationLease, kind string, data any) error {
	return r.WithSummaryGenerationLease(ctx, req, func(tx *Repositories) error {
		var run model.AgentRun
		if err := tx.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND user_id=? AND task_id=? AND subject_kind=?", req.GenerationID, req.UserID, req.TaskID, model.AgentRunSubjectSummaryGeneration).First(&run).Error; err != nil {
			return err
		}
		if !active(run.Status) {
			return artifact.Err("generation_stale", 409)
		}
		if payload, ok := data.(map[string]any); ok {
			if id, ok := payload["activity_id"].(string); ok && id != "publish" {
				var step model.AgentStep
				if err := tx.db.Where("run_id=? AND step_id=?", req.GenerationID, id).Order("attempt DESC").First(&step).Error; err != nil {
					return err
				}
				payload["attempt"] = step.Attempt
				payload["started_at"] = step.StartedAt
				if kind == "activity.finished" {
					now := time.Now().UTC()
					payload["finished_at"] = now
					payload["duration_ms"] = now.Sub(step.StartedAt).Milliseconds()
				}
			}
		}
		return appendEvent(tx.db, &run, kind, data)
	})
}

// Publication and the journal terminal event commit together. TaskJob remains
// the queue lease owner; the consumer completes that job after Generate returns.
func (r *Repositories) FinishSummaryGeneration(ctx context.Context, req PublishSummaryDocumentRequest, tags ...PrepareTagIntentRequest) (*model.AISummary, error) {
	return r.publishSummaryGeneration(ctx, req, false, tags...)
}
func (r *Repositories) PublishTextSummaryGeneration(ctx context.Context, req PublishSummaryDocumentRequest, tags ...PrepareTagIntentRequest) (*model.AISummary, error) {
	return r.publishSummaryGeneration(ctx, req, true, tags...)
}
func (r *Repositories) publishSummaryGeneration(ctx context.Context, req PublishSummaryDocumentRequest, continueRun bool, tagRequests ...PrepareTagIntentRequest) (*model.AISummary, error) {
	lease := SummaryGenerationLease{req.UserID, req.TaskID, req.GenerationID, req.SourceID, req.SourceDigest, req.LeaseToken}
	var summary *model.AISummary
	err := r.WithSummaryGenerationLease(ctx, lease, func(tx *Repositories) error {
		validation, err := tx.SummaryValidationContext(ctx, req.UserID, req.TaskID, req.SourceID, req.SourceDigest, req.GenerationID)
		if err != nil {
			return err
		}
		if err = summarydoc.ValidateGeneratedContent(req.Document, validation); err != nil {
			return artifact.Err("invalid_summary_document", 422)
		}
		row, err := tx.PublishSummaryDocument(ctx, req)
		if err != nil {
			return err
		}
		summary = row
		if _, err = tx.Task.SetGeneratedTitleIfBlank(req.TaskID, req.Document.Title); err != nil {
			return err
		}
		if len(tagRequests) > 0 && tx.UserTag != nil {
			tags := tagRequests[0]
			tags.GeneratedVersion = row.GeneratedVersion
			if _, err = tx.UserTag.PrepareTagIntent(ctx, tags); err != nil {
				return err
			}
		}
		var run model.AgentRun
		if err = tx.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND user_id=? AND task_id=? AND subject_kind=?", req.GenerationID, req.UserID, req.TaskID, model.AgentRunSubjectSummaryGeneration).First(&run).Error; err != nil {
			return err
		}
		if run.Status == model.AgentRunStatusCompleted {
			return nil
		}
		if !active(run.Status) {
			return artifact.Err("generation_stale", 409)
		}
		now := time.Now().UTC()
		if continueRun {
			if run.Stage == "text_ready" || run.Stage == "visual_enrichment" {
				return nil
			}
			if err = tx.db.Model(&run).Updates(map[string]any{"stage": "text_ready", "updated_at": now, "version": gorm.Expr("version + 1")}).Error; err != nil {
				return err
			}
			if err = appendEvent(tx.db, &run, "activity.finished", map[string]any{"activity_id": "publish", "attempt": 1, "kind": "save", "state": "done", "title": "文字摘要已保存", "finished_at": now}); err != nil {
				return err
			}
			return appendEvent(tx.db, &run, "run.text_ready", map[string]any{"status": "running", "stage": "text_ready", "text_state": "ready", "result_state": "ready", "generated_version": row.GeneratedVersion, "content_digest": row.ContentDigest})
		}
		if err = tx.db.Model(&run).Updates(map[string]any{"status": model.AgentRunStatusCompleted, "stage": "finalizing", "finished_at": now, "updated_at": now, "version": gorm.Expr("version + 1")}).Error; err != nil {
			return err
		}
		if err = appendEvent(tx.db, &run, "activity.finished", map[string]any{"activity_id": "publish", "attempt": 1, "kind": "save", "state": "done", "title": "完整摘要已保存", "finished_at": now}); err != nil {
			return err
		}
		return appendEvent(tx.db, &run, "run.completed", map[string]any{"status": "completed", "text_state": "ready", "result_state": "ready", "generated_version": row.GeneratedVersion, "content_digest": row.ContentDigest})
	})
	return summary, err
}

// CompleteSummaryGeneration closes optional enrichment using the latest result
// of this same generation. Source, queue lease and cancellation still fence it.
func (r *Repositories) CompleteSummaryGeneration(ctx context.Context, lease SummaryGenerationLease, visualState, reason string, tags *PrepareTagIntentRequest, tagFailure ...string) (*model.AISummary, error) {
	var summary *model.AISummary
	err := r.WithSummaryGenerationLease(ctx, lease, func(tx *Repositories) error {
		var run model.AgentRun
		if err := tx.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND user_id=? AND task_id=? AND subject_kind=?", lease.GenerationID, lease.UserID, lease.TaskID, model.AgentRunSubjectSummaryGeneration).First(&run).Error; err != nil {
			return err
		}
		row, err := tx.Summary.FindByTaskID(lease.TaskID)
		if err != nil {
			return err
		}
		if row == nil || row.GenerationID != lease.GenerationID || row.SourceID != lease.SourceID || row.SourceDigest != lease.SourceDigest {
			return artifact.Err("generation_stale", 409)
		}
		summary = row
		if run.Status == model.AgentRunStatusCompleted {
			return nil
		}
		if !active(run.Status) || run.CancelRequestedAt != nil {
			return artifact.Err("generation_cancelled", 409)
		}
		if tags != nil && tx.UserTag != nil {
			copy := *tags
			copy.GeneratedVersion = row.GeneratedVersion
			if _, err = tx.UserTag.PrepareTagIntent(ctx, copy); err != nil {
				return err
			}
			if len(tagFailure) > 0 && tagFailure[0] != "" {
				copy.LeaseToken = lease.LeaseToken
				if err = tx.UserTag.MarkTagIntentFailed(ctx, copy, tagFailure[0], false); err != nil {
					return err
				}
			}
		}
		now := time.Now().UTC()
		if err = tx.db.Model(&run).Updates(map[string]any{"status": model.AgentRunStatusCompleted, "stage": "finalizing", "finished_at": now, "updated_at": now, "version": gorm.Expr("version + 1")}).Error; err != nil {
			return err
		}
		var document struct {
			PresentationMode string `json:"presentation_mode"`
		}
		if err = json.Unmarshal([]byte(row.DocumentJSON), &document); err != nil {
			return err
		}
		return appendEvent(tx.db, &run, "run.completed", map[string]any{"status": "completed", "text_state": "ready", "result_state": "ready", "visual_state": visualState, "fallback_reason": reason, "requested_mode": run.Mode, "resolved_mode": document.PresentationMode, "generated_version": row.GeneratedVersion, "content_digest": row.ContentDigest})
	})
	return summary, err
}

func (r *Repositories) BeginSummaryVisualEnrichment(ctx context.Context, lease SummaryGenerationLease) error {
	return r.WithSummaryGenerationLease(ctx, lease, func(tx *Repositories) error {
		var run model.AgentRun
		if err := tx.db.Where("id=? AND user_id=? AND task_id=? AND subject_kind=?", lease.GenerationID, lease.UserID, lease.TaskID, model.AgentRunSubjectSummaryGeneration).First(&run).Error; err != nil {
			return err
		}
		if !active(run.Status) {
			return artifact.Err("generation_stale", 409)
		}
		if run.Stage == "visual_enrichment" {
			return nil
		}
		if err := tx.db.Model(&run).Updates(map[string]any{"stage": "visual_enrichment", "updated_at": time.Now().UTC(), "version": gorm.Expr("version + 1")}).Error; err != nil {
			return err
		}
		return appendEvent(tx.db, &run, "run.visual_started", map[string]any{"status": "running", "stage": "visual_enrichment", "text_state": "ready", "visual_state": "running"})
	})
}

// CloseSummaryGenerationActivities is called in the same transaction that
// terminalizes a run. Only activities which really began receive an end event.
func (r *Repositories) CloseSummaryGenerationActivities(run *model.AgentRun, state, code string, now time.Time) error {
	if run == nil || run.SubjectKind != model.AgentRunSubjectSummaryGeneration {
		return gorm.ErrInvalidData
	}
	var events []model.RunEvent
	if err := r.db.Where("run_id=? AND type IN ?", run.ID, []string{"activity.started", "activity.finished"}).Order("seq").Find(&events).Error; err != nil {
		return err
	}
	type activity struct {
		ID        string    `json:"activity_id"`
		Attempt   int       `json:"attempt"`
		StartedAt time.Time `json:"started_at"`
	}
	opened := map[string]activity{}
	for _, event := range events {
		var data activity
		if json.Unmarshal([]byte(event.DataJSON), &data) != nil || data.ID == "" {
			continue
		}
		if event.Type == "activity.started" {
			if data.StartedAt.IsZero() {
				data.StartedAt = event.CreatedAt
			}
			opened[data.ID] = data
		} else {
			if prior, ok := opened[data.ID]; ok && (data.Attempt == 0 || data.Attempt >= prior.Attempt) {
				delete(opened, data.ID)
			}
		}
	}
	if err := r.db.Model(&model.AgentStep{}).Where("run_id=? AND status=?", run.ID, model.AgentStepStatusRunning).Updates(map[string]any{"status": model.AgentStepStatusFailed, "error_code": code, "error_message": "", "lease_token": "", "lease_expires_at": nil, "finished_at": now, "updated_at": now}).Error; err != nil {
		return err
	}
	if err := r.db.Model(&model.AgentToolCall{}).Where("run_id=? AND status=?", run.ID, model.AgentToolCallStatusRunning).Updates(map[string]any{"status": model.AgentToolCallStatusFailed, "error_code": code, "error_message": "", "usage_source": model.AgentCallUsageUnknown, "finished_at": now, "updated_at": now}).Error; err != nil {
		return err
	}
	ids := make([]string, 0, len(opened))
	for id := range opened {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		data := opened[id]
		if err := appendEvent(r.db, run, "activity.finished", map[string]any{"activity_id": id, "attempt": max(1, data.Attempt), "state": state, "title": "处理已停止", "started_at": data.StartedAt, "finished_at": now, "duration_ms": max(0, now.Sub(data.StartedAt).Milliseconds())}); err != nil {
			return err
		}
	}
	return nil
}

// The summary job owns retries. Terminal queue failure and its run receipt share
// the queue CAS transaction, while retryable failure leaves checkpoints resumable.
func (r *Repositories) recordSummaryGenerationQueueFailure(job *model.TaskJob, req TaskProcessingFailureRequest) error {
	if job.GenerationID == "" {
		return nil
	}
	var run model.AgentRun
	err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND user_id=? AND task_id=? AND subject_kind=? AND subject_id=?", job.GenerationID, job.UserID, job.TaskID, model.AgentRunSubjectSummaryGeneration, job.GenerationID).First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !active(run.Status) {
		return nil
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	state := "error"
	code := "summary_generation_failed"
	// Queue wrappers may classify this as non_retryable_error. Preserve only
	// exact known causes, never copy free-form provider errors into public events.
	for _, cause := range []string{"budget_exhausted", "context_budget_exhausted", "duration_limit", "invalid_summary_document"} {
		if req.ErrorCode == cause || req.ErrorMessage == cause || (cause == "budget_exhausted" && strings.HasPrefix(req.ErrorMessage, "budget_exhausted: ")) {
			code = cause
			break
		}
	}
	if run.CancelRequestedAt != nil {
		state = "cancelled"
		code = "generation_cancelled"
	}
	if err = r.CloseSummaryGenerationActivities(&run, state, code, now); err != nil {
		return err
	}
	if req.NextRetryAt != nil && state != "cancelled" {
		return appendEvent(r.db, &run, "run.retry_waiting", map[string]any{"status": "running", "stage": run.Stage, "next_retry_at": req.NextRetryAt, "title": "等待重试"})
	}
	status := model.AgentRunStatusFailed
	kind := "run.failed"
	if state == "cancelled" {
		status = model.AgentRunStatusCancelled
		kind = "run.cancelled"
	}
	if req.Status == model.TaskStatusDead {
		code = "retry_exhausted"
	}
	if err = r.db.Model(&run).Updates(map[string]any{"status": status, "stop_reason": code, "error_code": code, "error_message": "", "finished_at": now, "updated_at": now, "version": gorm.Expr("version + 1")}).Error; err != nil {
		return err
	}
	return appendEvent(r.db, &run, kind, map[string]any{"status": status, "stage": run.Stage, "stop_reason": code, "finished_at": now})
}
