package repository

import (
	"context"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
	"vid-lens/internal/summarydoc"
)

type PrepareSummaryVisualRetryRequest struct {
	UserID, TaskID                                                                      int64
	ExpectedGenerationID, ExpectedSourceID, ExpectedSourceDigest, ExpectedContentDigest string
	ExpectedGeneratedVersion                                                            int64
	ExpectedIntentJSON                                                                  string
	Intent                                                                              processing.Intent
	Token                                                                               string
	Now, LeaseUntil                                                                     time.Time
}

func (r *Repositories) MarkSummaryVisualTextReused(ctx context.Context, lease SummaryGenerationLease, frozen processing.GenerationSnapshot) error {
	return r.WithSummaryGenerationLease(ctx, lease, func(tx *Repositories) error {
		var run model.AgentRun
		if err := tx.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", lease.GenerationID).First(&run).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.db.Model(&model.RunEvent{}).Where("run_id=? AND type=?", run.ID, "run.text_reused").Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		if err := appendEvent(tx.db, &run, "activity.started", map[string]any{"activity_id": "text-reuse", "attempt": 1, "kind": "reuse", "state": "running", "title": "读取已保存的文字摘要"}); err != nil {
			return err
		}
		if err := appendEvent(tx.db, &run, "run.text_reused", map[string]any{"text_state": "ready", "result_state": "ready", "operation": processing.OperationVisualRetry, "parent_generation_id": frozen.VisualRetry.ParentGenerationID, "title": "复用已保存的文字摘要", "generated_version": frozen.ExpectedGeneratedVersion, "content_digest": frozen.ExpectedGeneratedHash}); err != nil {
			return err
		}
		return appendEvent(tx.db, &run, "activity.finished", map[string]any{"activity_id": "text-reuse", "attempt": 1, "kind": "reuse", "state": "done", "title": "已复用完整文字摘要"})
	})
}

// Called inside the same transaction as the immutable HTTP acceptance receipt.
func (r *Repositories) PrepareSummaryVisualRetry(ctx context.Context, req PrepareSummaryVisualRetryRequest) (InitialTaskDispatch, error) {
	var prepared InitialTaskDispatch
	if req.Intent.GenerationID == req.ExpectedGenerationID || !req.Intent.Options.SummaryVisualEnabled {
		return prepared, artifact.Err("invalid_visual_retry_request", 400)
	}
	err := r.TransactionContext(ctx, func(tx *Repositories) error {
		task, err := summaryTask(tx.db, req.UserID, req.TaskID, true)
		if err != nil {
			return err
		}
		if task.ProcessingIntentJSON != req.ExpectedIntentJSON {
			return artifact.Err("generation_changed", 409)
		}
		if _, err = tx.TextSource.LockSource(ctx, req.UserID, req.TaskID, req.ExpectedSourceID, req.ExpectedSourceDigest); err != nil {
			return err
		}
		job, err := tx.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeSummary)
		if err != nil {
			return err
		}
		if SummaryJobActive(job) {
			return artifact.Err("summary_generation_active", 409)
		}
		var base model.AISummary
		if err = tx.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("task_id=?", task.ID).First(&base).Error; err != nil {
			return hideMissing(err)
		}
		if base.GenerationID != req.ExpectedGenerationID || base.GeneratedVersion != req.ExpectedGeneratedVersion || base.ContentDigest != req.ExpectedContentDigest || base.SourceID != req.ExpectedSourceID || base.SourceDigest != req.ExpectedSourceDigest || base.FileMD5 != task.FileMD5 || base.ContentHashKind != summarydoc.HashKind {
			return artifact.Err("version_conflict", 409)
		}
		doc, err := summarydoc.Parse([]byte(base.DocumentJSON))
		if err != nil || doc.PresentationMode != "text" {
			return artifact.Err("visual_retry_requires_text_result", 409)
		}
		validation, err := tx.SummaryValidationContext(ctx, req.UserID, task.ID, base.SourceID, base.SourceDigest, base.GenerationID)
		if err != nil {
			return err
		}
		if err = summarydoc.Validate(doc, validation); err != nil {
			return artifact.Err("invalid_summary_document", 422)
		}
		if _, err = processing.Decode(artifact.JSON(req.Intent)); err != nil {
			return artifact.Err("invalid_generation_snapshot", 400)
		}
		var priorRun model.AgentRun
		checkpointGeneration := base.GenerationID
		previousGeneration := base.GenerationID
		if job != nil && job.GenerationID != "" {
			var count int64
			if err = tx.db.Model(&model.AgentRun{}).Where("id=? AND user_id=? AND task_id=? AND subject_kind=?", job.GenerationID, req.UserID, task.ID, model.AgentRunSubjectSummaryGeneration).Count(&count).Error; err != nil {
				return err
			}
			if count == 1 {
				previousGeneration = job.GenerationID
				// A retry may only have reused the base run's immutable steps.
				// Keep that evidence reachable on a subsequent explicit attempt.
				var plans int64
				if err = tx.db.Model(&model.AgentStep{}).Where("run_id=? AND step_id IN ? AND status=?", job.GenerationID, []string{"visual-plan", "visual-plan-repair"}, model.AgentStepStatusCompleted).Count(&plans).Error; err != nil {
					return err
				}
				if plans > 0 {
					checkpointGeneration = job.GenerationID
				}
			}
		}
		if err = tx.db.Where("id=? AND user_id=? AND task_id=? AND subject_kind=?", previousGeneration, req.UserID, task.ID, model.AgentRunSubjectSummaryGeneration).First(&priorRun).Error; err != nil {
			return hideMissing(err)
		}
		spend := artifact.JSON(map[string]any{"run_id": priorRun.ID, "llm_calls": priorRun.LLMCallsUsed, "vision_calls": priorRun.VisionCallsUsed, "visual_calls": priorRun.VisualCallsUsed, "frames": priorRun.FramesUsed, "prompt_tokens": priorRun.PromptTokensUsed, "completion_tokens": priorRun.CompletionTokensUsed})
		snapshot := processing.GenerationSnapshot{Operation: processing.OperationVisualRetry, Intent: req.Intent, SourceID: base.SourceID, SourceDigest: base.SourceDigest, ExpectedGeneratedVersion: base.GeneratedVersion, ExpectedGeneratedHash: base.ContentDigest, ExpectedGeneratedHashKind: base.ContentHashKind, VisualRetry: &processing.VisualRetrySnapshot{ParentGenerationID: base.GenerationID, CheckpointGenerationID: checkpointGeneration, BaseDocumentJSON: base.DocumentJSON, BaseModelName: base.ModelName, PreviousSpendJSON: spend, NewBudgetAuthorized: true}}
		prepared, err = tx.PrepareInitialTaskDispatch(InitialTaskDispatchRequest{Task: task, AllowedStatuses: []int8{model.TaskStatusPending, model.TaskStatusCompleted, model.TaskStatusFailed, model.TaskStatusDead}, JobType: model.TaskJobTypeSummary, Stage: model.TaskStageSummarizing, SummaryForce: true, PreserveSummaryCheckpoints: true, Token: req.Token, Now: req.Now, LeaseUntil: req.LeaseUntil})
		if err != nil {
			return err
		}
		if err = tx.TaskJob.FreezeSummaryInput(task.ID, req.Intent.GenerationID, base.SourceID, artifact.JSON(snapshot)); err != nil {
			return err
		}
		raw := artifact.JSON(req.Intent)
		if err = tx.db.Model(task).Update("processing_intent_json", raw).Error; err != nil {
			return err
		}
		prepared.Task.ProcessingIntentJSON = raw
		return nil
	})
	return prepared, err
}

// Completion also supports failed enrichment: the unchanged parent result is
// still valid, rather than being relabelled as the new attempt's publication.
func (r *Repositories) CompleteSummaryVisualRetry(ctx context.Context, lease SummaryGenerationLease, frozen processing.GenerationSnapshot, state, reason string) error {
	return r.WithSummaryGenerationLease(ctx, lease, func(tx *Repositories) error {
		var run model.AgentRun
		if err := tx.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND user_id=? AND task_id=? AND subject_kind=?", lease.GenerationID, lease.UserID, lease.TaskID, model.AgentRunSubjectSummaryGeneration).First(&run).Error; err != nil {
			return err
		}
		if run.Status == model.AgentRunStatusCompleted {
			return nil
		}
		if !active(run.Status) || run.CancelRequestedAt != nil {
			return artifact.Err("generation_cancelled", 409)
		}
		base, err := tx.Summary.FindByTaskID(lease.TaskID)
		if err != nil {
			return err
		}
		if base == nil || base.SourceID != lease.SourceID || base.SourceDigest != lease.SourceDigest {
			return artifact.Err("generation_stale", 409)
		}
		if base.GenerationID != lease.GenerationID && (base.GenerationID != frozen.VisualRetry.ParentGenerationID || base.GeneratedVersion != frozen.ExpectedGeneratedVersion || base.ContentDigest != frozen.ExpectedGeneratedHash) {
			return artifact.Err("generation_stale", 409)
		}
		doc, err := summarydoc.Parse([]byte(base.DocumentJSON))
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if err = tx.CloseSummaryGenerationActivities(&run, "error", reason, now); err != nil {
			return err
		}
		if err = tx.db.Model(&run).Updates(map[string]any{"status": model.AgentRunStatusCompleted, "stage": "finalizing", "finished_at": now, "updated_at": now, "version": gorm.Expr("version + 1")}).Error; err != nil {
			return err
		}
		return appendEvent(tx.db, &run, "run.completed", map[string]any{"status": "completed", "text_state": "ready", "result_state": "ready", "visual_state": state, "fallback_reason": reason, "requested_mode": run.Mode, "resolved_mode": doc.PresentationMode, "result_generation_id": base.GenerationID, "generated_version": base.GeneratedVersion, "content_digest": base.ContentDigest})
	})
}
