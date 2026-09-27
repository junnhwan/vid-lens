package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

type ArtifactCall struct {
	Step   model.AgentStep
	Call   model.AgentToolCall
	Cached string
}

// BeginCall reserves cumulative budget before the provider is invoked. One registered call = one attempt.
func (r *ArtifactRepository) BeginCall(ctx context.Context, id, token string, epoch int64, stepID, digest string, prompt, output int64) (*ArtifactCall, error) {
	out := &ArtifactCall{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		run, err := lockedRun(tx, id)
		if err != nil {
			return err
		}
		if err = fence(run, token, epoch, time.Now().UTC()); err != nil {
			return err
		}
		if run.CancelRequestedAt != nil {
			return artifact.ErrLease
		}
		var previous model.AgentStep
		err = tx.Where("run_id=? AND step_id=?", id, stepID).Order("attempt DESC").First(&previous).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil && previous.Status == "completed" {
			if previous.InputSummary != digest || previous.ResultCheckpoint == "" {
				return artifact.Err("unsupported_checkpoint", 409)
			}
			out.Cached = previous.ResultCheckpoint
			return nil
		}
		if previous.Status == "failed" && previous.ErrorCode == "invalid_model_output" {
			return artifact.Err("format_repair_required", 422)
		}
		if previous.Attempt >= run.MaxAttemptsPerStep {
			return artifact.Err("budget_exhausted", 422)
		}
		if previous.RetryNotBefore != nil && previous.RetryNotBefore.After(time.Now().UTC()) {
			return &artifact.RetryWait{Until: *previous.RetryNotBefore}
		}
		if run.ExecutionStartedAt == nil || time.Since(*run.ExecutionStartedAt).Milliseconds() >= run.MaxDurationMs || run.LLMCallsUsed >= run.MaxLLMCalls || run.PromptTokensUsed+prompt > run.MaxPromptTokens || run.CompletionTokensUsed+output > run.MaxCompletionTokens {
			return artifact.Err("budget_exhausted", 422)
		}
		now := time.Now().UTC()
		step := model.AgentStep{ID: uuid.NewString(), RunID: id, StepID: stepID, Attempt: previous.Attempt + 1, Sequence: run.LLMCallsUsed + 1, Kind: "artifact", Action: "generate_study", Status: "running", InputSummary: digest, LeaseToken: uuid.NewString(), LeaseExpiresAt: run.RunLeaseUntil, StartedAt: now, EstimatedPromptTokens: prompt}
		call := model.AgentToolCall{ID: uuid.NewString(), RunID: id, StepID: stepID, Attempt: step.Attempt, AgentStepID: step.ID, CallKind: model.AgentCallKindPlannerLLM, ToolName: "study_generate", Status: "running", InputSummary: "{}", ArgumentsDigest: digest, CallDigest: digest, PromptTokens: prompt, CompletionTokens: output, UsageSource: "estimated", TokenEstimated: true, StartedAt: now}
		if err = tx.Create(&step).Error; err != nil {
			return err
		}
		if err = tx.Create(&call).Error; err != nil {
			return err
		}
		if err = tx.Model(run).Updates(map[string]any{"llm_calls_used": gorm.Expr("llm_calls_used+1"), "prompt_tokens_used": gorm.Expr("prompt_tokens_used+?", prompt), "completion_tokens_used": gorm.Expr("completion_tokens_used+?", output), "token_usage_source": "estimated", "stage": "generating"}).Error; err != nil {
			return err
		}
		out.Step = step
		out.Call = call
		return appendEvent(tx, run, "run.updated", map[string]any{"status": "running", "stage": "generating", "llm_calls": run.LLMCallsUsed + 1})
	})
	return out, err
}

func (r *ArtifactRepository) FailCall(ctx context.Context, id, token string, epoch int64, call *ArtifactCall, code string, retryAt ...*time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		run, err := lockedRun(tx, id)
		if err != nil {
			return err
		}
		if err = fence(run, token, epoch, time.Now().UTC()); err != nil {
			return err
		}
		now := time.Now().UTC()
		var next *time.Time
		if len(retryAt) > 0 {
			next = retryAt[0]
		}
		if err = tx.Model(&model.AgentStep{}).Where("id=? AND lease_token=? AND status='running'", call.Step.ID, call.Step.LeaseToken).Updates(map[string]any{"status": "failed", "error_code": code, "finished_at": now, "retry_not_before": next}).Error; err != nil {
			return err
		}
		return tx.Model(&model.AgentToolCall{}).Where("id=?", call.Call.ID).Updates(map[string]any{"status": "failed", "error_code": code, "finished_at": now}).Error
	})
}

// SettleCall may run after cancellation/lease loss. It only adjusts a registered call's usage,
// never result content or terminal state. Repeated provider_call_id settlement is a no-op.
func (r *ArtifactRepository) SettleCall(ctx context.Context, callID string, prompt, completion int64, actual bool) error {
	var lookup model.AgentToolCall
	if err := r.db.WithContext(ctx).Where("id=?", callID).First(&lookup).Error; err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		run, err := lockedRun(tx, lookup.RunID)
		if err != nil {
			return err
		}
		var call model.AgentToolCall
		if err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", callID).First(&call).Error; err != nil {
			return err
		}
		var metrics struct {
			Settled bool `json:"settled"`
		}
		_ = json.Unmarshal([]byte(call.MetricsJSON), &metrics)
		if metrics.Settled || !actual {
			return nil
		}
		if prompt < 0 || completion < 0 {
			return gorm.ErrInvalidData
		}
		promptDelta, completionDelta := prompt-call.PromptTokens, completion-call.CompletionTokens
		if err = tx.Model(&call).Updates(map[string]any{"prompt_tokens": prompt, "completion_tokens": completion, "usage_source": "actual", "token_estimated": false, "metrics_json": "{\"settled\":true}"}).Error; err != nil {
			return err
		}
		var estimates int64
		if err = tx.Model(&model.AgentToolCall{}).Where("run_id=? AND usage_source<>'actual'", run.ID).Count(&estimates).Error; err != nil {
			return err
		}
		source := "actual"
		if estimates > 0 {
			source = "mixed"
		}
		return tx.Model(run).Updates(map[string]any{"prompt_tokens_used": gorm.Expr("prompt_tokens_used+?", promptDelta), "completion_tokens_used": gorm.Expr("completion_tokens_used+?", completionDelta), "token_usage_source": source}).Error
	})
}
func (r *ArtifactRepository) Checkpoint(ctx context.Context, id, token string, epoch int64, call *ArtifactCall, result string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		run, err := lockedRun(tx, id)
		if err != nil {
			return err
		}
		if err = fence(run, token, epoch, time.Now().UTC()); err != nil {
			return err
		}
		if run.CancelRequestedAt != nil {
			return artifact.ErrLease
		}
		now := time.Now().UTC()
		res := tx.Model(&model.AgentStep{}).Where("id=? AND lease_token=? AND status='running'", call.Step.ID, call.Step.LeaseToken).Updates(map[string]any{"status": "completed", "result_checkpoint": result, "result_digest": artifact.Hash(result), "finished_at": now})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return artifact.ErrLease
		}
		return tx.Model(&model.AgentToolCall{}).Where("id=?", call.Call.ID).Updates(map[string]any{"status": "completed", "finished_at": now}).Error
	})
}
func (r *ArtifactRepository) TaskRows(ctx context.Context, owner int64, page, size int) ([]model.AgentRun, []model.VideoTask, int64, error) {
	runs := []model.AgentRun{}
	videos := []model.VideoTask{}
	var total int64
	const projection = "SELECT 'artifact' AS kind, id AS resource_id, created_at FROM agent_runs WHERE user_id=? AND subject_kind='generation_request' UNION ALL SELECT 'video' AS kind, CAST(id AS TEXT) AS resource_id, created_at FROM video_tasks WHERE user_id=? AND deleted_at IS NULL"
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Raw("SELECT COUNT(*) FROM ("+projection+") AS tasks", owner, owner).Scan(&total).Error; err != nil {
			return err
		}
		var pageRows []struct {
			Kind       string
			ResourceID string
		}
		if err := tx.Raw("SELECT kind,resource_id FROM ("+projection+") AS tasks ORDER BY created_at DESC,kind,resource_id LIMIT ? OFFSET ?", owner, owner, size, (page-1)*size).Scan(&pageRows).Error; err != nil {
			return err
		}
		runIDs, videoIDs := []string{}, []string{}
		for _, row := range pageRows {
			if row.Kind == "artifact" {
				runIDs = append(runIDs, row.ResourceID)
			} else {
				videoIDs = append(videoIDs, row.ResourceID)
			}
		}
		if len(runIDs) > 0 {
			if err := tx.Where("id IN ? AND user_id=?", runIDs, owner).Find(&runs).Error; err != nil {
				return err
			}
		}
		if len(videoIDs) > 0 {
			return tx.Select("id,user_id,title,filename,status,stage,created_at,updated_at").Where("id IN ? AND user_id=?", videoIDs, owner).Find(&videos).Error
		}
		return nil
	})
	return runs, videos, total, err
}
