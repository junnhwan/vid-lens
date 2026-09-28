package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

type SummaryDispatchIntent struct {
	model.SummaryEditDispatch
	SubjectKind string
}

func (r *SummaryRevisionRepository) RunSubject(ctx context.Context, runID string) (string, error) {
	var run model.AgentRun
	if err := r.db.WithContext(ctx).Where("id = ? AND execution_kind = ?", runID, "artifact").First(&run).Error; err != nil {
		return "", hideMissing(err)
	}
	return run.SubjectKind, nil
}

// Recover queues a new delivery only after the previous delivery has had
// time to arrive, or an execution lease has expired. The provider step itself
// is journaled and replays under the frozen operation identity.
func (r *SummaryRevisionRepository) Recover(ctx context.Context) error {
	now := time.Now().UTC()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var runs []model.AgentRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("subject_kind = ? AND (status = ? OR (status = ? AND (run_lease_until IS NULL OR run_lease_until <= ?)))", model.AgentRunSubjectSummaryEdit, model.AgentRunStatusPending, model.AgentRunStatusRunning, now).Order("created_at").Limit(100).Find(&runs).Error; err != nil {
			return err
		}
		for _, run := range runs {
			var op model.SummaryEditOperation
			if err := tx.Where("run_id = ?", run.ID).First(&op).Error; err != nil {
				return err
			}
			if op.Status != "running" {
				continue
			}
			var latest model.SummaryEditDispatch
			err := tx.Where("run_id = ?", run.ID).Order("created_at DESC").First(&latest).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err == nil && (latest.PublishedAt == nil || latest.CreatedAt.After(now.Add(-time.Minute))) {
				continue
			}
			if err := tx.Create(&model.SummaryEditDispatch{ID: uuid.NewString(), RunID: run.ID, NextAttemptAt: now, CreatedAt: now}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *SummaryRevisionRepository) Dispatches(ctx context.Context) ([]SummaryDispatchIntent, error) {
	now := time.Now().UTC()
	var out []SummaryDispatchIntent
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []model.SummaryEditDispatch
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("published_at IS NULL AND next_attempt_at <= ? AND (lease_until IS NULL OR lease_until <= ?)", now, now).Order("created_at").Limit(30).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			token, until := uuid.NewString(), now.Add(30*time.Second)
			if err := tx.Model(&row).Updates(map[string]any{"lease_token": token, "lease_until": until}).Error; err != nil {
				return err
			}
			row.LeaseToken, row.LeaseUntil = token, &until
			out = append(out, SummaryDispatchIntent{SummaryEditDispatch: row, SubjectKind: model.AgentRunSubjectSummaryEdit})
		}
		return nil
	})
	return out, err
}

func (r *SummaryRevisionRepository) DispatchResult(ctx context.Context, row model.SummaryEditDispatch, published bool) error {
	now := time.Now().UTC()
	values := map[string]any{"lease_until": nil, "next_attempt_at": now.Add(5 * time.Second)}
	if published {
		values["published_at"] = now
	}
	return r.db.WithContext(ctx).Model(&model.SummaryEditDispatch{}).Where("id = ? AND lease_token = ?", row.ID, row.LeaseToken).Updates(values).Error
}

func (r *SummaryRevisionRepository) ClaimRun(ctx context.Context, runID, token string, duration time.Duration) (*model.SummaryEditOperation, bool, error) {
	if runID == "" || token == "" {
		return nil, false, artifact.Err("invalid_request", 400)
	}
	var out *model.SummaryEditOperation
	claimed := false
	now := time.Now().UTC()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var op model.SummaryEditOperation
		if err := tx.Where("run_id = ?", runID).First(&op).Error; err != nil {
			return hideMissing(err)
		}
		if _, err := summaryTask(tx, op.UserID, op.TaskID, true); err != nil {
			return err
		}
		var run model.AgentRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ? AND subject_kind = ?", runID, op.UserID, model.AgentRunSubjectSummaryEdit).First(&run).Error; err != nil {
			return hideMissing(err)
		}
		if run.Status != model.AgentRunStatusPending && run.Status != model.AgentRunStatusRunning {
			return nil
		}
		if op.Status != "running" || run.CancelRequestedAt != nil {
			return nil
		}
		if run.RunLeaseUntil != nil && run.RunLeaseUntil.After(now) {
			return nil
		}
		if duration <= 0 {
			duration = 2 * time.Minute
		}
		updates := map[string]any{"status": model.AgentRunStatusRunning, "stage": "planning", "run_lease_token": token, "run_lease_epoch": run.RunLeaseEpoch + 1, "run_lease_until": now.Add(duration), "version": run.Version + 1, "updated_at": now}
		if run.ExecutionStartedAt == nil {
			updates["execution_started_at"] = now
		}
		if err := tx.Model(&run).Updates(updates).Error; err != nil {
			return err
		}
		out, claimed = &op, true
		return nil
	})
	return out, claimed, err
}

func (r *SummaryRevisionRepository) RenewRun(ctx context.Context, runID, token string, duration time.Duration) (bool, error) {
	now := time.Now().UTC()
	result := r.db.WithContext(ctx).Model(&model.AgentRun{}).Where("id = ? AND subject_kind = ? AND status = ? AND run_lease_token = ? AND run_lease_until > ?", runID, model.AgentRunSubjectSummaryEdit, model.AgentRunStatusRunning, token, now).Updates(map[string]any{"run_lease_until": now.Add(duration), "updated_at": now})
	return result.RowsAffected == 1, result.Error
}

func (r *SummaryRevisionRepository) ReleaseRun(ctx context.Context, runID, token string) error {
	return r.db.WithContext(ctx).Model(&model.AgentRun{}).Where("id = ? AND subject_kind = ? AND run_lease_token = ?", runID, model.AgentRunSubjectSummaryEdit, token).Updates(map[string]any{"run_lease_token": "", "run_lease_until": nil, "updated_at": time.Now().UTC()}).Error
}

func summaryLeaseValid(tx *gorm.DB, runID, token string) error {
	if token == "" {
		return nil
	}
	var run model.AgentRun
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND subject_kind = ?", runID, model.AgentRunSubjectSummaryEdit).First(&run).Error; err != nil {
		return hideMissing(err)
	}
	if run.Status != model.AgentRunStatusRunning || run.CancelRequestedAt != nil || run.RunLeaseToken != token || run.RunLeaseUntil == nil || !run.RunLeaseUntil.After(time.Now().UTC()) {
		return artifact.Err("lease_lost", 409)
	}
	return nil
}
