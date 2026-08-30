package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"vid-lens/internal/model"
)

const (
	AgentStepClaimAcquired  = "acquired"
	AgentStepClaimBusy      = "busy"
	AgentStepClaimCompleted = "completed"
	AgentStepClaimTerminal  = "terminal"
	AgentStepClaimAmbiguous = "ambiguous"
	AgentStepClaimExhausted = "budget_exhausted"
)

type AgentExecutionRepository struct {
	db *gorm.DB
}

type AgentStepClaimRequest struct {
	UserID                int64
	RunID                 string
	StepID                string
	Attempt               int
	Sequence              int
	Kind                  string
	Action                string
	SafeReason            string
	InputSummary          string
	ArgumentsDigest       string
	CallDigest            string
	ToolName              string
	CallKind              string
	InternalCall          bool
	ReplaySafe            bool
	LLMCall               bool
	VisionCall            bool
	RetrievalCall         bool
	VisualCall            bool
	FrameCount            int
	ContextChars          int64
	EstimatedPromptTokens int64
	LeaseToken            string
	Now                   time.Time
	LeaseUntil            time.Time
}

type AgentStepClaim struct {
	Outcome  string
	Run      model.AgentRun
	Step     model.AgentStep
	ToolCall *model.AgentToolCall
}

type AgentStepCompletion struct {
	UserID             int64
	RunID              string
	StepID             string
	Attempt            int
	LeaseToken         string
	OutputRef          string
	ResultCheckpoint   string
	EvidenceRefs       string
	MetricsJSON        string
	PromptTokens       int64
	CompletionTokens   int64
	CostMicros         int64
	UsageSource        string
	TokenEstimated     bool
	Currency           string
	PriceVersion       string
	ContextChars       int64
	ContextUsageSource string
	Now                time.Time
}

type AgentStepFailure struct {
	UserID             int64
	RunID              string
	StepID             string
	Attempt            int
	LeaseToken         string
	ErrorCode          string
	ErrorMessage       string
	Ambiguous          bool
	Cancelled          bool
	PromptTokens       int64
	CompletionTokens   int64
	CostMicros         int64
	UsageSource        string
	TokenEstimated     bool
	Currency           string
	PriceVersion       string
	ContextChars       int64
	ContextUsageSource string
	MetricsJSON        string
	Now                time.Time
}

type AgentRunTerminalUpdate struct {
	UserID       int64
	RunID        string
	Status       string
	StopReason   string
	ErrorCode    string
	ErrorMessage string
	Now          time.Time
}

type AgentExecutionRecords struct {
	Run       model.AgentRun        `json:"run"`
	Steps     []model.AgentStep     `json:"steps"`
	ToolCalls []model.AgentToolCall `json:"tool_calls"`
}

func NewAgentExecutionRepository(db *gorm.DB) *AgentExecutionRepository {
	return &AgentExecutionRepository{db: db}
}

func (r *AgentExecutionRepository) CreateRun(ctx context.Context, run *model.AgentRun) (bool, error) {
	if r == nil || r.db == nil || run == nil {
		return false, gorm.ErrInvalidDB
	}
	if err := validateAgentRun(run); err != nil {
		return false, err
	}
	now := run.CreatedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	run.CreatedAt, run.UpdatedAt = now, now
	if run.Status == "" {
		run.Status = model.AgentRunStatusRunning
	}
	if run.Version <= 0 {
		run.Version = 1
	}
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(run)
	return result.RowsAffected == 1, result.Error
}

func (r *AgentExecutionRepository) GetRun(ctx context.Context, userID int64, runID string) (*model.AgentRun, error) {
	if r == nil || r.db == nil || userID <= 0 || strings.TrimSpace(runID) == "" {
		return nil, gorm.ErrInvalidData
	}
	var run model.AgentRun
	err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", strings.TrimSpace(runID), userID).First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *AgentExecutionRepository) GetExecution(ctx context.Context, userID int64, runID string) (*AgentExecutionRecords, error) {
	run, err := r.GetRun(ctx, userID, runID)
	if err != nil || run == nil {
		return nil, err
	}
	records := &AgentExecutionRecords{Run: *run, Steps: []model.AgentStep{}, ToolCalls: []model.AgentToolCall{}}
	if err := r.db.WithContext(ctx).Where("run_id = ?", run.ID).Order("sequence ASC, attempt ASC").Find(&records.Steps).Error; err != nil {
		return nil, err
	}
	if err := r.db.WithContext(ctx).Table("agent_tool_calls AS c").Select("c.*").
		Joins("JOIN agent_steps AS s ON s.id = c.agent_step_id").
		Where("c.run_id = ?", run.ID).Order("s.sequence ASC, c.attempt ASC, c.id ASC").Scan(&records.ToolCalls).Error; err != nil {
		return nil, err
	}
	return records, nil
}

// MarkFinalEvidenceRefs projects the final citation selection onto each
// completed call. It never changes the call's original evidence set.
func (r *AgentExecutionRepository) MarkFinalEvidenceRefs(ctx context.Context, userID int64, runID string, finalRefs []string) error {
	if r == nil || r.db == nil || userID <= 0 || strings.TrimSpace(runID) == "" {
		return gorm.ErrInvalidData
	}
	wanted := make(map[string]struct{}, len(finalRefs))
	for _, ref := range finalRefs {
		if ref = strings.TrimSpace(ref); ref != "" {
			wanted[ref] = struct{}{}
		}
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run model.AgentRun
		if err := tx.Where("id = ? AND user_id = ?", strings.TrimSpace(runID), userID).First(&run).Error; err != nil {
			return err
		}
		var calls []model.AgentToolCall
		if err := tx.Where("run_id = ? AND status = ?", run.ID, model.AgentToolCallStatusCompleted).Find(&calls).Error; err != nil {
			return err
		}
		for _, call := range calls {
			var observed []string
			if err := json.Unmarshal([]byte(defaultJSON(call.EvidenceRefs, "[]")), &observed); err != nil {
				return err
			}
			selected := make([]string, 0, len(observed))
			for _, ref := range observed {
				if _, ok := wanted[ref]; ok {
					selected = append(selected, ref)
				}
			}
			encoded, _ := json.Marshal(selected)
			if err := tx.Model(&model.AgentToolCall{}).Where("id = ?", call.ID).Update("final_evidence_refs", string(encoded)).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// ClaimStep creates or recovers one step attempt under the frozen run budget.
// An expired non-replayable call becomes ambiguous instead of being invoked a
// second time with the same idempotency tuple.
func (r *AgentExecutionRepository) ClaimStep(ctx context.Context, req AgentStepClaimRequest) (AgentStepClaim, error) {
	if r == nil || r.db == nil {
		return AgentStepClaim{}, gorm.ErrInvalidDB
	}
	if err := validateAgentStepClaim(req); err != nil {
		return AgentStepClaim{}, err
	}
	var claim AgentStepClaim
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run model.AgentRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", req.RunID, req.UserID).First(&run).Error; err != nil {
			return err
		}
		claim.Run = run
		if agentRunTerminal(run.Status) {
			claim.Outcome = AgentStepClaimTerminal
			return nil
		}

		var existing model.AgentStep
		err := tx.Where("run_id = ? AND step_id = ? AND attempt = ?", req.RunID, req.StepID, req.Attempt).First(&existing).Error
		if err == nil {
			claim.Step = existing
			toolCall, toolErr := findAgentToolCall(tx, req.RunID, req.StepID, req.Attempt)
			if toolErr != nil {
				return toolErr
			}
			claim.ToolCall = toolCall
			switch existing.Status {
			case model.AgentStepStatusCompleted:
				claim.Outcome = AgentStepClaimCompleted
				return nil
			case model.AgentStepStatusFailed, model.AgentStepStatusAmbiguous:
				claim.Outcome = AgentStepClaimTerminal
				return nil
			}
			if existing.LeaseExpiresAt != nil && existing.LeaseExpiresAt.After(req.Now) {
				claim.Outcome = AgentStepClaimBusy
				return nil
			}
			if !existing.ReplaySafe {
				finished := req.Now
				updated := tx.Model(&model.AgentStep{}).
					Where("id = ? AND status = ? AND lease_version = ?", existing.ID, model.AgentStepStatusRunning, existing.LeaseVersion).
					Updates(map[string]any{"status": model.AgentStepStatusAmbiguous, "error_code": "interrupted_non_replayable", "error_message": "non-replayable call lease expired before a terminal result was persisted", "lease_token": "", "lease_expires_at": nil, "finished_at": finished, "updated_at": finished})
				if updated.Error != nil {
					return updated.Error
				}
				if updated.RowsAffected != 1 {
					claim.Outcome = AgentStepClaimBusy
					return nil
				}
				_ = tx.Model(&model.AgentToolCall{}).Where("agent_step_id = ? AND status = ?", existing.ID, model.AgentToolCallStatusRunning).
					Updates(map[string]any{"status": model.AgentToolCallStatusAmbiguous, "error_code": "interrupted_non_replayable", "error_message": "non-replayable call lease expired before a terminal result was persisted", "finished_at": finished, "updated_at": finished}).Error
				existing.Status = model.AgentStepStatusAmbiguous
				existing.ErrorCode = "interrupted_non_replayable"
				existing.ErrorMessage = "non-replayable call lease expired before a terminal result was persisted"
				existing.FinishedAt = &finished
				claim.Step, claim.Outcome = existing, AgentStepClaimAmbiguous
				return nil
			}
			updated := tx.Model(&model.AgentStep{}).
				Where("id = ? AND status = ? AND lease_version = ?", existing.ID, model.AgentStepStatusRunning, existing.LeaseVersion).
				Updates(map[string]any{"lease_token": req.LeaseToken, "lease_expires_at": req.LeaseUntil, "lease_version": existing.LeaseVersion + 1, "updated_at": req.Now})
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				claim.Outcome = AgentStepClaimBusy
				return nil
			}
			existing.LeaseToken, existing.LeaseExpiresAt, existing.LeaseVersion = req.LeaseToken, &req.LeaseUntil, existing.LeaseVersion+1
			claim.Step, claim.Outcome = existing, AgentStepClaimAcquired
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if req.Attempt > run.MaxAttemptsPerStep {
			claim.Outcome = AgentStepClaimExhausted
			return markAgentRunBudgetExhausted(tx, &claim.Run, req.Now, "attempt_budget_exhausted")
		}
		if req.Attempt > 1 {
			var prior model.AgentStep
			if err := tx.Where("run_id = ? AND step_id = ? AND attempt = ?", req.RunID, req.StepID, req.Attempt-1).First(&prior).Error; err != nil {
				return fmt.Errorf("agent step attempt must be contiguous: %w", err)
			}
			if prior.Status != model.AgentStepStatusFailed && prior.Status != model.AgentStepStatusAmbiguous {
				return fmt.Errorf("agent step prior attempt is not retryable: %s", prior.Status)
			}
			if !prior.ReplaySafe || !req.ReplaySafe {
				claim.Step = prior
				claim.ToolCall, err = findAgentToolCall(tx, req.RunID, req.StepID, prior.Attempt)
				if err != nil {
					return err
				}
				if prior.Status == model.AgentStepStatusAmbiguous {
					claim.Outcome = AgentStepClaimAmbiguous
				} else {
					claim.Outcome = AgentStepClaimTerminal
				}
				return nil
			}
			priorCall, err := findAgentToolCall(tx, req.RunID, req.StepID, prior.Attempt)
			if err != nil {
				return err
			}
			callKind := strings.TrimSpace(req.CallKind)
			if callKind == "" {
				callKind = model.AgentCallKindTool
			}
			callMatches := priorCall == nil && strings.TrimSpace(req.ToolName) == ""
			if priorCall != nil {
				callMatches = priorCall.ToolName == req.ToolName && priorCall.CallKind == callKind && priorCall.ArgumentsDigest == req.ArgumentsDigest
			}
			if prior.Sequence != req.Sequence || prior.Kind != req.Kind || prior.Action != req.Action || !callMatches {
				return errors.New("agent step retry does not match prior attempt input")
			}
		}
		if agentBudgetExceeded(run, req) {
			claim.Outcome = AgentStepClaimExhausted
			return markAgentRunBudgetExhausted(tx, &claim.Run, req.Now, "budget_exhausted")
		}

		step := model.AgentStep{
			ID: uuid.NewString(), RunID: req.RunID, StepID: req.StepID, Attempt: req.Attempt,
			Sequence: req.Sequence, Kind: req.Kind, Action: req.Action, Status: model.AgentStepStatusRunning,
			SafeReason: req.SafeReason, InputSummary: defaultJSON(req.InputSummary, "{}"), ReplaySafe: req.ReplaySafe,
			LeaseToken: req.LeaseToken, LeaseExpiresAt: &req.LeaseUntil, LeaseVersion: 1,
			StartedAt: req.Now, EstimatedPromptTokens: req.EstimatedPromptTokens, CreatedAt: req.Now, UpdatedAt: req.Now,
		}
		if err := tx.Create(&step).Error; err != nil {
			return err
		}
		var toolCall *model.AgentToolCall
		if strings.TrimSpace(req.ToolName) != "" {
			callKind := strings.TrimSpace(req.CallKind)
			if callKind == "" {
				callKind = model.AgentCallKindTool
			}
			call := model.AgentToolCall{
				ID: uuid.NewString(), RunID: req.RunID, StepID: req.StepID, Attempt: req.Attempt, AgentStepID: step.ID,
				CallKind: callKind, ToolName: req.ToolName, Status: model.AgentToolCallStatusRunning,
				InputSummary: defaultJSON(req.InputSummary, "{}"), ArgumentsDigest: req.ArgumentsDigest, CallDigest: req.CallDigest,
				EvidenceRefs: "[]", FinalEvidenceRefs: "[]", MetricsJSON: "{}", StartedAt: req.Now, CreatedAt: req.Now, UpdatedAt: req.Now,
			}
			if err := tx.Create(&call).Error; err != nil {
				return err
			}
			toolCall = &call
		}
		updates := map[string]any{
			"status": model.AgentRunStatusRunning, "steps_used": gorm.Expr("steps_used + 1"),
			"version": gorm.Expr("version + 1"), "updated_at": req.Now,
		}
		if toolCall != nil && !req.InternalCall {
			updates["tool_calls_used"] = gorm.Expr("tool_calls_used + 1")
		}
		if req.LLMCall {
			updates["llm_calls_used"] = gorm.Expr("llm_calls_used + 1")
		}
		if req.VisionCall {
			updates["vision_calls_used"] = gorm.Expr("vision_calls_used + 1")
		}
		if req.RetrievalCall {
			updates["retrieval_calls_used"] = gorm.Expr("retrieval_calls_used + 1")
		}
		if req.VisualCall {
			updates["visual_calls_used"] = gorm.Expr("visual_calls_used + 1")
		}
		if req.FrameCount > 0 {
			updates["frames_used"] = gorm.Expr("frames_used + ?", req.FrameCount)
		}
		if req.ContextChars > 0 {
			updates["context_chars_used"] = gorm.Expr("context_chars_used + ?", req.ContextChars)
			updates["context_usage_source"] = model.AgentCallUsageEstimated
		}
		if req.EstimatedPromptTokens > 0 {
			updates["prompt_tokens_used"] = gorm.Expr("prompt_tokens_used + ?", req.EstimatedPromptTokens)
			updates["token_usage_source"] = model.AgentCallUsageEstimated
		}
		updates["duration_ms_used"] = gorm.Expr("CASE WHEN duration_ms_used < ? THEN ? ELSE duration_ms_used END", durationMillis(run.CreatedAt, req.Now), durationMillis(run.CreatedAt, req.Now))
		updated := tx.Model(&model.AgentRun{}).Where("id = ? AND user_id = ? AND version = ? AND status IN ?", run.ID, req.UserID, run.Version, []string{model.AgentRunStatusPending, model.AgentRunStatusRunning}).Updates(updates)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return errors.New("agent run budget CAS failed")
		}
		claim.Outcome, claim.Step, claim.ToolCall = AgentStepClaimAcquired, step, toolCall
		claim.Run = run
		claim.Run.Status = model.AgentRunStatusRunning
		claim.Run.Version++
		claim.Run.StepsUsed++
		if toolCall != nil && !req.InternalCall {
			claim.Run.ToolCallsUsed++
		}
		if req.LLMCall {
			claim.Run.LLMCallsUsed++
		}
		if req.VisionCall {
			claim.Run.VisionCallsUsed++
		}
		if req.RetrievalCall {
			claim.Run.RetrievalCallsUsed++
		}
		if req.VisualCall {
			claim.Run.VisualCallsUsed++
		}
		claim.Run.FramesUsed += req.FrameCount
		claim.Run.PromptTokensUsed += req.EstimatedPromptTokens
		claim.Run.ContextCharsUsed += req.ContextChars
		if req.EstimatedPromptTokens > 0 {
			claim.Run.TokenUsageSource = model.AgentCallUsageEstimated
		}
		claim.Run.DurationMsUsed = maxInt64(claim.Run.DurationMsUsed, durationMillis(run.CreatedAt, req.Now))
		if req.ContextChars > 0 {
			claim.Run.ContextUsageSource = model.AgentCallUsageEstimated
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AgentStepClaim{}, nil
	}
	return claim, err
}

func (r *AgentExecutionRepository) CompleteStep(ctx context.Context, req AgentStepCompletion) (bool, error) {
	if r == nil || r.db == nil || req.UserID <= 0 || req.RunID == "" || req.StepID == "" || req.Attempt <= 0 || req.LeaseToken == "" {
		return false, gorm.ErrInvalidData
	}
	if req.PromptTokens < 0 || req.CompletionTokens < 0 || req.CostMicros < 0 || req.ContextChars < 0 {
		return false, gorm.ErrInvalidData
	}
	if req.Now.IsZero() {
		req.Now = time.Now().UTC()
	}
	digest := digestText(req.ResultCheckpoint)
	changed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run model.AgentRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ? AND status IN ?", req.RunID, req.UserID, []string{model.AgentRunStatusPending, model.AgentRunStatusRunning}).First(&run).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		var step model.AgentStep
		if err := tx.Where("run_id = ? AND step_id = ? AND attempt = ?", req.RunID, req.StepID, req.Attempt).First(&step).Error; err != nil {
			return err
		}
		updated := tx.Model(&model.AgentStep{}).Where("id = ? AND status = ? AND lease_token = ?", step.ID, model.AgentStepStatusRunning, req.LeaseToken).
			Updates(map[string]any{"status": model.AgentStepStatusCompleted, "output_ref": req.OutputRef, "result_checkpoint": req.ResultCheckpoint, "result_digest": digest, "lease_token": "", "lease_expires_at": nil, "finished_at": req.Now, "duration_ms": durationMillis(step.StartedAt, req.Now), "updated_at": req.Now})
		if updated.Error != nil || updated.RowsAffected != 1 {
			return updated.Error
		}
		changed = true
		usageSource := strings.TrimSpace(req.UsageSource)
		if usageSource == "" {
			usageSource = model.AgentCallUsageUnknown
		}
		if err := tx.Model(&model.AgentToolCall{}).Where("agent_step_id = ? AND status = ?", step.ID, model.AgentToolCallStatusRunning).
			Updates(map[string]any{"status": model.AgentToolCallStatusCompleted, "output_ref": req.OutputRef, "result_checkpoint": req.ResultCheckpoint, "result_digest": digest, "evidence_refs": defaultJSON(req.EvidenceRefs, "[]"), "metrics_json": enrichAgentMetrics(req.MetricsJSON, req.ContextChars, req.ContextUsageSource, usageSource, req.CostMicros), "prompt_tokens": req.PromptTokens, "completion_tokens": req.CompletionTokens, "cost_micros": req.CostMicros, "usage_source": usageSource, "token_estimated": req.TokenEstimated, "currency": strings.TrimSpace(req.Currency), "price_version": strings.TrimSpace(req.PriceVersion), "context_chars": req.ContextChars, "context_usage_source": defaultUsageSource(req.ContextUsageSource, model.AgentCallUsageUnknown), "finished_at": req.Now, "duration_ms": durationMillis(step.StartedAt, req.Now), "updated_at": req.Now}).Error; err != nil {
			return err
		}
		usageUpdates := agentRunUsageUpdates(&run, promptTokenDelta(step.EstimatedPromptTokens, req.PromptTokens), req.CompletionTokens, req.CostMicros, usageSource, req.ContextChars, req.ContextUsageSource, durationMillis(run.CreatedAt, req.Now))
		runUpdate := tx.Model(&model.AgentRun{}).Where("id = ? AND user_id = ? AND version = ?", run.ID, req.UserID, run.Version).Updates(usageUpdates)
		if runUpdate.Error != nil {
			return runUpdate.Error
		}
		if runUpdate.RowsAffected != 1 {
			return errors.New("agent run usage CAS failed")
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return changed, err
}

func (r *AgentExecutionRepository) FailStep(ctx context.Context, req AgentStepFailure) (bool, error) {
	if r == nil || r.db == nil || req.UserID <= 0 || req.RunID == "" || req.StepID == "" || req.Attempt <= 0 || req.LeaseToken == "" {
		return false, gorm.ErrInvalidData
	}
	if req.PromptTokens < 0 || req.CompletionTokens < 0 || req.CostMicros < 0 || req.ContextChars < 0 {
		return false, gorm.ErrInvalidData
	}
	if req.Now.IsZero() {
		req.Now = time.Now().UTC()
	}
	status, toolStatus := model.AgentStepStatusFailed, model.AgentToolCallStatusFailed
	if req.Ambiguous {
		status, toolStatus = model.AgentStepStatusAmbiguous, model.AgentToolCallStatusAmbiguous
	}
	changed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run model.AgentRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ? AND status IN ?", req.RunID, req.UserID, []string{model.AgentRunStatusPending, model.AgentRunStatusRunning}).First(&run).Error; err != nil {
			return err
		}
		var step model.AgentStep
		if err := tx.Where("run_id = ? AND step_id = ? AND attempt = ?", req.RunID, req.StepID, req.Attempt).First(&step).Error; err != nil {
			return err
		}
		updated := tx.Model(&model.AgentStep{}).Where("id = ? AND status = ? AND lease_token = ?", step.ID, model.AgentStepStatusRunning, req.LeaseToken).
			Updates(map[string]any{"status": status, "error_code": req.ErrorCode, "error_message": req.ErrorMessage, "lease_token": "", "lease_expires_at": nil, "finished_at": req.Now, "duration_ms": durationMillis(step.StartedAt, req.Now), "updated_at": req.Now})
		if updated.Error != nil || updated.RowsAffected != 1 {
			return updated.Error
		}
		changed = true
		usageSource := strings.TrimSpace(req.UsageSource)
		if usageSource == "" {
			usageSource = model.AgentCallUsageUnknown
		}
		if err := tx.Model(&model.AgentToolCall{}).Where("agent_step_id = ? AND status = ?", step.ID, model.AgentToolCallStatusRunning).
			Updates(map[string]any{"status": toolStatus, "error_code": req.ErrorCode, "error_message": req.ErrorMessage, "prompt_tokens": req.PromptTokens, "completion_tokens": req.CompletionTokens, "cost_micros": req.CostMicros, "usage_source": usageSource, "token_estimated": req.TokenEstimated, "currency": strings.TrimSpace(req.Currency), "price_version": strings.TrimSpace(req.PriceVersion), "context_chars": req.ContextChars, "context_usage_source": defaultUsageSource(req.ContextUsageSource, model.AgentCallUsageUnknown), "metrics_json": enrichAgentMetrics(req.MetricsJSON, req.ContextChars, req.ContextUsageSource, usageSource, req.CostMicros), "finished_at": req.Now, "duration_ms": durationMillis(step.StartedAt, req.Now), "updated_at": req.Now}).Error; err != nil {
			return err
		}
		runUpdate := tx.Model(&model.AgentRun{}).Where("id = ? AND user_id = ? AND version = ?", run.ID, req.UserID, run.Version).Updates(agentRunUsageUpdates(&run, promptTokenDelta(step.EstimatedPromptTokens, req.PromptTokens), req.CompletionTokens, req.CostMicros, usageSource, req.ContextChars, req.ContextUsageSource, durationMillis(run.CreatedAt, req.Now)))
		if runUpdate.Error != nil {
			return runUpdate.Error
		}
		if runUpdate.RowsAffected != 1 {
			return errors.New("agent run usage CAS failed")
		}
		run.Version++
		if req.Cancelled {
			return markAgentRunCancelled(tx, &run, req.Now, "request_cancelled", req.ErrorCode, req.ErrorMessage)
		}
		if step.ReplaySafe && req.Attempt >= run.MaxAttemptsPerStep {
			return markAgentRunBudgetExhausted(tx, &run, req.Now, "attempt_budget_exhausted")
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return changed, err
}

// MarkRunTerminal is monotonic: normal retries can transition only a live run,
// and can never overwrite an existing terminal result.
func (r *AgentExecutionRepository) MarkRunTerminal(ctx context.Context, req AgentRunTerminalUpdate) (bool, error) {
	if r == nil || r.db == nil || req.UserID <= 0 || req.RunID == "" || !agentRunTerminal(req.Status) {
		return false, gorm.ErrInvalidData
	}
	if req.Now.IsZero() {
		req.Now = time.Now().UTC()
	}
	result := r.db.WithContext(ctx).Model(&model.AgentRun{}).
		Where("id = ? AND user_id = ? AND status IN ?", req.RunID, req.UserID, []string{model.AgentRunStatusPending, model.AgentRunStatusRunning}).
		Updates(map[string]any{"status": req.Status, "stop_reason": req.StopReason, "error_code": req.ErrorCode, "error_message": req.ErrorMessage, "finished_at": req.Now, "updated_at": req.Now, "version": gorm.Expr("version + 1")})
	return result.RowsAffected == 1, result.Error
}

func (r *AgentExecutionRepository) ListExpiredRunning(ctx context.Context, now time.Time, limit int) ([]model.AgentStep, error) {
	if r == nil || r.db == nil || limit <= 0 {
		return []model.AgentStep{}, nil
	}
	var steps []model.AgentStep
	err := r.db.WithContext(ctx).Table("agent_steps AS s").Select("s.*").
		Joins("JOIN agent_runs AS r ON r.id = s.run_id").
		Where("s.status = ? AND s.lease_expires_at <= ? AND r.status IN ?", model.AgentStepStatusRunning, now, []string{model.AgentRunStatusPending, model.AgentRunStatusRunning}).
		Order("s.lease_expires_at ASC, s.created_at ASC").Limit(limit).Scan(&steps).Error
	return steps, err
}

func validateAgentRun(run *model.AgentRun) error {
	if strings.TrimSpace(run.ID) == "" || run.UserID <= 0 || run.SessionID <= 0 || strings.TrimSpace(run.ScopeType) == "" || strings.TrimSpace(run.Goal) == "" || strings.TrimSpace(run.Mode) == "" {
		return gorm.ErrInvalidData
	}
	if run.ScopeType == model.ChatScopeVideo && (run.TaskID <= 0 || run.KnowledgeBaseID != 0) {
		return gorm.ErrInvalidData
	}
	if run.ScopeType == model.ChatScopeKnowledgeBase && (run.KnowledgeBaseID <= 0 || run.TaskID != 0) {
		return gorm.ErrInvalidData
	}
	if run.MaxSteps <= 0 || run.MaxToolCalls < 0 || run.MaxLLMCalls < 0 || run.MaxVisionCalls < 0 || run.MaxAttemptsPerStep <= 0 || run.MaxRetrievalCalls < 0 || run.MaxVisualCalls < 0 || run.MaxFrames < 0 || run.MaxPromptTokens < 0 || run.MaxCompletionTokens < 0 || run.MaxCostMicros < 0 || run.MaxDurationMs < 0 || run.MaxContextChars < 0 {
		return gorm.ErrInvalidData
	}
	if !jsonObjectOrArray(run.ProfileSnapshot) || !jsonObjectOrArray(run.PolicySnapshot) || !jsonObjectOrArray(run.BudgetSnapshot) {
		return gorm.ErrInvalidData
	}
	return nil
}

func validateAgentStepClaim(req AgentStepClaimRequest) error {
	if req.UserID <= 0 || strings.TrimSpace(req.RunID) == "" || strings.TrimSpace(req.StepID) == "" || req.Attempt <= 0 || req.Sequence <= 0 || strings.TrimSpace(req.Kind) == "" || strings.TrimSpace(req.Action) == "" || strings.TrimSpace(req.LeaseToken) == "" || req.Now.IsZero() || !req.LeaseUntil.After(req.Now) {
		return gorm.ErrInvalidData
	}
	if strings.TrimSpace(req.ToolName) != "" && (len(req.ArgumentsDigest) != 64 || len(req.CallDigest) != 64) {
		return gorm.ErrInvalidData
	}
	if req.InternalCall && (strings.TrimSpace(req.ToolName) == "" || strings.TrimSpace(req.CallKind) == "") {
		return gorm.ErrInvalidData
	}
	if req.CallKind != "" && req.CallKind != model.AgentCallKindTool && req.CallKind != model.AgentCallKindPlannerLLM && req.CallKind != model.AgentCallKindValidation {
		return gorm.ErrInvalidData
	}
	if !jsonObjectOrArray(defaultJSON(req.InputSummary, "{}")) {
		return gorm.ErrInvalidData
	}
	if req.FrameCount < 0 || req.ContextChars < 0 || req.EstimatedPromptTokens < 0 {
		return gorm.ErrInvalidData
	}
	return nil
}

func agentBudgetExceeded(run model.AgentRun, req AgentStepClaimRequest) bool {
	if run.StepsUsed >= run.MaxSteps {
		return true
	}
	if strings.TrimSpace(req.ToolName) != "" && !req.InternalCall && run.ToolCallsUsed >= run.MaxToolCalls {
		return true
	}
	if req.LLMCall && run.LLMCallsUsed >= run.MaxLLMCalls {
		return true
	}
	if req.VisionCall && run.VisionCallsUsed >= run.MaxVisionCalls {
		return true
	}
	if req.RetrievalCall && run.MaxRetrievalCalls > 0 && run.RetrievalCallsUsed >= run.MaxRetrievalCalls {
		return true
	}
	if req.VisualCall && run.MaxVisualCalls > 0 && run.VisualCallsUsed >= run.MaxVisualCalls {
		return true
	}
	if req.FrameCount > 0 && run.MaxFrames > 0 && run.FramesUsed+req.FrameCount > run.MaxFrames {
		return true
	}
	if run.MaxPromptTokens > 0 && run.PromptTokensUsed+req.EstimatedPromptTokens > run.MaxPromptTokens {
		return true
	}
	if run.MaxCompletionTokens > 0 && run.CompletionTokensUsed >= run.MaxCompletionTokens {
		return true
	}
	if run.MaxCostMicros > 0 && run.CostMicrosUsed >= run.MaxCostMicros {
		return true
	}
	if run.MaxDurationMs > 0 && durationMillis(run.CreatedAt, req.Now) >= run.MaxDurationMs {
		return true
	}
	return run.MaxContextChars > 0 && run.ContextCharsUsed+req.ContextChars > run.MaxContextChars
}

func markAgentRunBudgetExhausted(tx *gorm.DB, run *model.AgentRun, now time.Time, reason string) error {
	result := tx.Model(&model.AgentRun{}).Where("id = ? AND version = ? AND status IN ?", run.ID, run.Version, []string{model.AgentRunStatusPending, model.AgentRunStatusRunning}).
		Updates(map[string]any{"status": model.AgentRunStatusBudgetExhausted, "stop_reason": reason, "finished_at": now, "updated_at": now, "version": gorm.Expr("version + 1")})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("agent run terminal CAS failed")
	}
	run.Status, run.StopReason, run.FinishedAt, run.Version = model.AgentRunStatusBudgetExhausted, reason, &now, run.Version+1
	return nil
}

func markAgentRunCancelled(tx *gorm.DB, run *model.AgentRun, now time.Time, reason, errorCode, errorMessage string) error {
	result := tx.Model(&model.AgentRun{}).Where("id = ? AND version = ? AND status IN ?", run.ID, run.Version, []string{model.AgentRunStatusPending, model.AgentRunStatusRunning}).
		Updates(map[string]any{"status": model.AgentRunStatusCancelled, "stop_reason": reason, "error_code": errorCode, "error_message": errorMessage, "finished_at": now, "updated_at": now, "version": gorm.Expr("version + 1")})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("agent run terminal CAS failed")
	}
	run.Status, run.StopReason, run.ErrorCode, run.ErrorMessage, run.FinishedAt, run.Version = model.AgentRunStatusCancelled, reason, errorCode, errorMessage, &now, run.Version+1
	return nil
}

func findAgentToolCall(tx *gorm.DB, runID, stepID string, attempt int) (*model.AgentToolCall, error) {
	var call model.AgentToolCall
	err := tx.Where("run_id = ? AND step_id = ? AND attempt = ?", runID, stepID, attempt).First(&call).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &call, nil
}

func ownedActiveAgentRun(tx *gorm.DB, userID int64, runID string) bool {
	var count int64
	_ = tx.Model(&model.AgentRun{}).Where("id = ? AND user_id = ? AND status IN ?", runID, userID, []string{model.AgentRunStatusPending, model.AgentRunStatusRunning}).Count(&count).Error
	return count == 1
}

func agentRunTerminal(status string) bool {
	switch status {
	case model.AgentRunStatusCompleted, model.AgentRunStatusFailed, model.AgentRunStatusCancelled, model.AgentRunStatusBudgetExhausted:
		return true
	default:
		return false
	}
}

func digestText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func durationMillis(start, end time.Time) int64 {
	if start.IsZero() || end.Before(start) {
		return 0
	}
	return end.Sub(start).Milliseconds()
}

func defaultJSON(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func jsonObjectOrArray(value string) bool {
	var decoded any
	if json.Unmarshal([]byte(value), &decoded) != nil {
		return false
	}
	switch decoded.(type) {
	case map[string]any, []any:
		return true
	default:
		return false
	}
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func defaultUsageSource(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func mergeUsageSource(previous, current string, present bool) string {
	if !present {
		return defaultUsageSource(previous, model.AgentCallUsageUnknown)
	}
	current = defaultUsageSource(current, model.AgentCallUsageUnknown)
	previous = defaultUsageSource(previous, model.AgentCallUsageUnknown)
	if previous == model.AgentCallUsageUnknown {
		return current
	}
	if current == model.AgentCallUsageUnknown || previous == current {
		return previous
	}
	return model.AgentCallUsageMixed
}

func agentRunUsageUpdates(run *model.AgentRun, promptTokens, completionTokens, costMicros int64, usageSource string, _ int64, contextUsageSource string, durationMs int64) map[string]any {
	updates := map[string]any{
		"prompt_tokens_used":     gorm.Expr("prompt_tokens_used + ?", promptTokens),
		"completion_tokens_used": gorm.Expr("completion_tokens_used + ?", completionTokens),
		"cost_micros_used":       gorm.Expr("cost_micros_used + ?", costMicros),
		"duration_ms_used":       gorm.Expr("CASE WHEN duration_ms_used < ? THEN ? ELSE duration_ms_used END", durationMs, durationMs),
		"version":                gorm.Expr("version + 1"),
	}
	if promptTokens > 0 || completionTokens > 0 {
		updates["token_usage_source"] = mergeUsageSource(run.TokenUsageSource, usageSource, true)
	}
	if costMicros > 0 {
		updates["cost_usage_source"] = mergeUsageSource(run.CostUsageSource, usageSource, true)
	}
	if strings.TrimSpace(contextUsageSource) != "" {
		updates["context_usage_source"] = mergeUsageSource(run.ContextUsageSource, contextUsageSource, true)
	}
	return updates
}

func promptTokenDelta(reserved, reported int64) int64 {
	if reserved <= 0 {
		return reported
	}
	if reported <= 0 {
		return 0
	}
	return reported - reserved
}

func enrichAgentMetrics(raw string, contextChars int64, contextSource, usageSource string, costMicros int64) string {
	metrics := map[string]any{}
	if json.Unmarshal([]byte(defaultJSON(raw, "{}")), &metrics) != nil {
		metrics = map[string]any{}
	}
	if contextChars > 0 {
		metrics["context_chars"] = contextChars
		metrics["context_usage_source"] = defaultUsageSource(contextSource, model.AgentCallUsageUnknown)
	}
	if usageSource == model.AgentCallUsageEstimated || usageSource == model.AgentCallUsageActual {
		metrics["token_usage_source"] = usageSource
	}
	if costMicros > 0 {
		metrics["cost_usage_source"] = usageSource
	} else {
		metrics["cost_usage_source"] = model.AgentCallUsageUnknown
	}
	encoded, err := json.Marshal(metrics)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}
