package repository

import (
	"context"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"strings"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
)

// The adapter reuses immutable successful visual evidence, never an old run's
// write authority or counters. All journal writes still target the new attempt.
type SummaryVisualCheckpointStore struct {
	*SummaryGenerationExecutionStore
	lease  SummaryGenerationLease
	parent string
}

func NewSummaryVisualCheckpointStore(repos *Repositories, lease SummaryGenerationLease, parent string) *SummaryVisualCheckpointStore {
	return &SummaryVisualCheckpointStore{NewSummaryGenerationExecutionStore(repos, lease.UserID, lease.TaskID, lease.GenerationID), lease, parent}
}

func (s *SummaryVisualCheckpointStore) ClaimStep(ctx context.Context, req AgentStepClaimRequest) (AgentStepClaim, error) {
	if s.parent == "" || req.RunID != s.lease.GenerationID || req.UserID != s.lease.UserID || (req.StepID != "visual-plan" && req.StepID != "visual-plan-repair" && !strings.HasPrefix(req.StepID, "visual-inspect-")) {
		return s.SummaryGenerationExecutionStore.ClaimStep(ctx, req)
	}
	var saved AgentStepClaim
	err := s.repos.WithSummaryGenerationLease(ctx, s.lease, func(tx *Repositories) error {
		current, err := NewSummaryGenerationExecutionStore(tx, s.owner, s.taskID, s.generationID).GetRun(ctx, s.owner, s.generationID)
		if err != nil {
			return err
		}
		if current == nil || !active(current.Status) {
			return nil
		}
		var local int64
		if err = tx.db.Model(&model.AgentStep{}).Where("run_id=? AND step_id=?", req.RunID, req.StepID).Count(&local).Error; err != nil || local > 0 {
			return err
		}
		parent, err := NewSummaryGenerationExecutionStore(tx, s.owner, s.taskID, s.parent).GetRun(ctx, s.owner, s.parent)
		if err != nil {
			return err
		}
		if parent == nil || parent.ProfileSnapshot != current.ProfileSnapshot || parent.RecipeVersion != current.RecipeVersion {
			return nil
		}
		var oldPolicy, newPolicy struct {
			SourceID     string `json:"source_id"`
			SourceDigest string `json:"source_digest"`
		}
		if json.Unmarshal([]byte(parent.PolicySnapshot), &oldPolicy) != nil || json.Unmarshal([]byte(current.PolicySnapshot), &newPolicy) != nil || oldPolicy != newPolicy || oldPolicy.SourceID != s.lease.SourceID || oldPolicy.SourceDigest != s.lease.SourceDigest {
			return nil
		}
		if strings.HasPrefix(req.StepID, "visual-plan") {
			var terminal model.RunEvent
			if err = tx.db.Where("run_id=? AND type=?", s.parent, "run.completed").Order("seq DESC").First(&terminal).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			var outcome struct {
				Reason string `json:"fallback_reason"`
			}
			_ = json.Unmarshal([]byte(terminal.DataJSON), &outcome)
			if outcome.Reason == "invalid_visual_plan" || outcome.Reason == "invalid_visual_response" {
				return nil
			}
		}
		var step model.AgentStep
		err = tx.db.Where("run_id=? AND step_id=? AND status=?", s.parent, req.StepID, model.AgentStepStatusCompleted).Order("attempt DESC").First(&step).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if !step.ReplaySafe || step.Action != req.Action || step.Sequence != req.Sequence || step.Kind != req.Kind {
			return nil
		}
		var invalid map[string]any
		if json.Unmarshal([]byte(step.ResultCheckpoint), &invalid) == nil && invalid["invalid_visual_response"] == true {
			return nil
		}
		call, err := findAgentToolCall(tx.db, s.parent, req.StepID, step.Attempt)
		if err != nil {
			return err
		}
		if call == nil || call.Status != model.AgentToolCallStatusCompleted || call.ArgumentsDigest != req.ArgumentsDigest || call.ToolName != req.ToolName || call.CallKind != req.CallKind {
			return nil
		}
		// Only the in-memory digest changes to describe this validated reuse.
		// The original provider call and old run/steps remain byte-for-byte intact.
		copyCall := *call
		copyCall.CallDigest = req.CallDigest
		saved = AgentStepClaim{Outcome: AgentStepClaimCompleted, Run: *current, Step: step, ToolCall: &copyCall}
		var finished []model.RunEvent
		if err = tx.db.Where("run_id=? AND type=?", current.ID, "activity.finished").Find(&finished).Error; err != nil {
			return err
		}
		for _, event := range finished {
			var receipt struct {
				ActivityID string `json:"activity_id"`
				Kind       string `json:"kind"`
				Parent     string `json:"parent_generation_id"`
			}
			if json.Unmarshal([]byte(event.DataJSON), &receipt) == nil && receipt.ActivityID == req.StepID && receipt.Kind == "reuse" && receipt.Parent == s.parent {
				return nil
			}
		}
		if err = appendEvent(tx.db, current, "activity.started", map[string]any{"activity_id": req.StepID, "attempt": 1, "kind": "reuse", "state": "running", "title": "读取已保存的画面检查结果", "parent_generation_id": s.parent}); err != nil {
			return err
		}
		return appendEvent(tx.db, current, "activity.finished", map[string]any{"activity_id": req.StepID, "attempt": 1, "kind": "reuse", "state": "done", "title": "已复用完成的画面检查", "parent_generation_id": s.parent, "operation": processing.OperationVisualRetry})
	})
	if err != nil {
		return AgentStepClaim{}, err
	}
	if saved.Outcome == AgentStepClaimCompleted {
		return saved, nil
	}
	return s.SummaryGenerationExecutionStore.ClaimStep(ctx, req)
}
