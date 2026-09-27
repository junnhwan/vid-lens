package repository

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

func active(s string) bool { return s == "pending" || s == "running" }
func appendEvent(tx *gorm.DB, run *model.AgentRun, kind string, data any) error {
	run.EventSeq++
	if err := tx.Model(&model.AgentRun{}).Where("id=?", run.ID).Update("event_seq", run.EventSeq).Error; err != nil {
		return err
	}
	return tx.Create(&model.RunEvent{RunID: run.ID, Seq: run.EventSeq, Type: kind, DataJSON: artifact.JSON(data)}).Error
}
func (r *ArtifactRepository) ByKey(ctx context.Context, owner int64, key, hash string) (*model.GenerationRequest, error) {
	var req model.GenerationRequest
	err := r.db.WithContext(ctx).Where("user_id=? AND idempotency_key=?", owner, key).First(&req).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if req.RequestHash != hash {
		return nil, artifact.Err("idempotency_conflict", 409)
	}
	return &req, nil
}
func (r *ArtifactRepository) Submit(ctx context.Context, req *model.GenerationRequest, run *model.AgentRun, title string) (*model.GenerationRequest, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		m, err := readableManifest(tx, req.UserID, req.ManifestID)
		if err != nil {
			return err
		}
		if _, err = sourceLock(tx, req.UserID, m.SourceID); err != nil {
			return err
		}
		// Source lock serializes submissions for this video; the unique owner/key constraint handles different sources.
		prior, err := NewArtifactRepository(tx).ByKey(ctx, req.UserID, req.IdempotencyKey, req.RequestHash)
		if err != nil {
			return err
		}
		if prior != nil {
			*req = *prior
			return nil
		}
		if req.ArtifactID == "" {
			a := model.Artifact{ID: uuid.NewString(), UserID: req.UserID, Kind: "study", Title: title}
			if err = tx.Create(&a).Error; err != nil {
				return err
			}
			req.ArtifactID = a.ID
		} else {
			a, err := ownedArtifact(tx, req.UserID, req.ArtifactID, true)
			if err != nil {
				return err
			}
			if a.HeadVersion != req.BaseVersion {
				return artifact.Err("version_conflict", 409)
			}
		}
		if err = tx.Create(req).Error; err != nil {
			return err
		}
		run.SubjectID = req.ID
		if err = tx.Create(run).Error; err != nil {
			return err
		}
		if err = appendEvent(tx, run, "run.created", map[string]any{"status": "pending", "stage": "queued"}); err != nil {
			return err
		}
		return tx.Create(&model.GenerationDispatch{ID: uuid.NewString(), RunID: run.ID, NextAttemptAt: time.Now().UTC()}).Error
	})
	if err != nil {
		if prior, e := r.ByKey(ctx, req.UserID, req.IdempotencyKey, req.RequestHash); e != nil {
			return nil, e
		} else if prior != nil {
			return prior, nil
		}
	}
	return req, err
}
func (r *ArtifactRepository) Run(ctx context.Context, owner int64, id string) (*model.AgentRun, *model.GenerationRequest, error) {
	var run model.AgentRun
	var req model.GenerationRequest
	q := r.db.WithContext(ctx).Where("id=? AND subject_kind='generation_request'", id)
	if owner > 0 {
		q = q.Where("user_id=?", owner)
	}
	if err := q.First(&run).Error; err != nil {
		return nil, nil, hideMissing(err)
	}
	if err := r.db.WithContext(ctx).Where("run_id=?", id).First(&req).Error; err != nil {
		return nil, nil, err
	}
	return &run, &req, nil
}
func lockedRun(tx *gorm.DB, id string) (*model.AgentRun, error) {
	var run model.AgentRun
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND subject_kind='generation_request'", id).First(&run).Error
	return &run, hideMissing(err)
}
func fence(run *model.AgentRun, token string, epoch int64, now time.Time) error {
	if run.Status != "running" || run.RunLeaseToken != token || run.RunLeaseEpoch != epoch || run.RunLeaseUntil == nil || !run.RunLeaseUntil.After(now) {
		return artifact.ErrLease
	}
	return nil
}
func (r *ArtifactRepository) Claim(ctx context.Context, id, token string, now time.Time) (*model.AgentRun, error) {
	now = now.UTC().Truncate(time.Microsecond)
	var run *model.AgentRun
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		run, err = lockedRun(tx, id)
		if err != nil {
			return err
		}
		if !active(run.Status) || (run.Status == "running" && run.RunLeaseUntil != nil && run.RunLeaseUntil.After(now)) {
			return artifact.ErrLease
		}
		var req model.GenerationRequest
		if err = tx.Where("run_id=?", id).First(&req).Error; err != nil {
			return err
		}
		if run.CancelRequestedAt != nil {
			return terminal(tx, run, "cancelled", "")
		}
		if run.ExecutionStartedAt == nil && now.After(req.QueueDeadline) {
			return terminal(tx, run, "failed", "queue_expired")
		}
		if run.ExecutionStartedAt != nil && now.Sub(*run.ExecutionStartedAt).Milliseconds() >= run.MaxDurationMs {
			return terminal(tx, run, "budget_exhausted", "budget_exhausted")
		}
		run.Status = "running"
		run.Stage = "collecting"
		run.RunLeaseToken = token
		run.RunLeaseEpoch++
		until := now.Add(30 * time.Second)
		run.RunLeaseUntil = &until
		if run.ExecutionStartedAt == nil {
			run.ExecutionStartedAt = &now
		}
		if err = tx.Model(run).Updates(map[string]any{"status": run.Status, "stage": run.Stage, "run_lease_token": token, "run_lease_epoch": run.RunLeaseEpoch, "run_lease_until": until, "execution_started_at": run.ExecutionStartedAt}).Error; err != nil {
			return err
		}
		// A previous external invocation may have succeeded remotely. Preserve conservative reservations.
		if err = tx.Model(&model.AgentStep{}).Where("run_id=? AND status='running'", id).Updates(map[string]any{"status": "ambiguous", "error_code": "worker_interrupted", "finished_at": now}).Error; err != nil {
			return err
		}
		if err = tx.Model(&model.AgentToolCall{}).Where("run_id=? AND status='running'", id).Updates(map[string]any{"status": "ambiguous", "error_code": "worker_interrupted", "finished_at": now}).Error; err != nil {
			return err
		}
		return appendEvent(tx, run, "run.updated", map[string]any{"status": "running", "stage": "collecting"})
	})
	return run, err
}
func terminal(tx *gorm.DB, run *model.AgentRun, status, code string) error {
	now := time.Now().UTC()
	run.Status = status
	run.Stage = status
	run.ErrorCode = code
	run.FinishedAt = &now
	if err := tx.Model(run).Updates(map[string]any{"status": status, "stage": status, "error_code": code, "finished_at": now, "run_lease_token": "", "run_lease_until": nil}).Error; err != nil {
		return err
	}
	kind := "run.failed"
	if status == "completed" {
		kind = "run.completed"
	}
	if status == "cancelled" {
		kind = "run.cancelled"
	}
	return appendEvent(tx, run, kind, map[string]any{"status": status, "stage": status, "error_code": code, "version_id": run.ResultVersionID})
}
func (r *ArtifactRepository) Heartbeat(ctx context.Context, id, token string, epoch int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		run, err := lockedRun(tx, id)
		if err != nil {
			return err
		}
		if err = fence(run, token, epoch, time.Now().UTC()); err != nil {
			return err
		}
		if run.CancelRequestedAt != nil {
			if err = terminal(tx, run, "cancelled", ""); err != nil {
				return err
			}
			return nil
		}
		return tx.Model(run).Update("run_lease_until", time.Now().UTC().Add(30*time.Second)).Error
	})
}
func (r *ArtifactRepository) Finish(ctx context.Context, id, token string, epoch int64, status, code string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		run, err := lockedRun(tx, id)
		if err != nil {
			return err
		}
		if err = fence(run, token, epoch, time.Now().UTC()); err != nil {
			return err
		}
		if run.CancelRequestedAt != nil {
			status = "cancelled"
			code = ""
		}
		return terminal(tx, run, status, code)
	})
}

func (r *ArtifactRepository) Progress(ctx context.Context, id, token string, epoch int64, stage string, covered, total int) error {
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
		if err = tx.Model(run).Update("stage", stage).Error; err != nil {
			return err
		}
		return appendEvent(tx, run, "run.updated", map[string]any{"status": "running", "stage": stage, "covered_segments": covered, "total_segments": total})
	})
}
func (r *ArtifactRepository) Cancel(ctx context.Context, owner int64, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		run, err := lockedRun(tx, id)
		if err != nil {
			return err
		}
		if run.UserID != owner {
			return artifact.Err("not_found", 404)
		}
		if !active(run.Status) || run.CancelRequestedAt != nil {
			return nil
		}
		now := time.Now().UTC()
		if err = tx.Model(run).Update("cancel_requested_at", now).Error; err != nil {
			return err
		}
		if err = appendEvent(tx, run, "run.cancel_requested", map[string]any{"status": run.Status}); err != nil {
			return err
		}
		if run.Status == "pending" {
			return terminal(tx, run, "cancelled", "")
		}
		return nil
	})
}
func (r *ArtifactRepository) Resume(ctx context.Context, owner int64, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		run, err := lockedRun(tx, id)
		if err != nil {
			return err
		}
		if run.UserID != owner {
			return artifact.Err("not_found", 404)
		}
		if !active(run.Status) {
			return artifact.Err("run_terminal", 409)
		}
		return queueDispatch(tx, id, time.Now().UTC())
	})
}
func queueDispatch(tx *gorm.DB, id string, now time.Time) error {
	var n int64
	if err := tx.Model(&model.GenerationDispatch{}).Where("run_id=? AND (published_at IS NULL OR created_at>?)", id, now.Add(-30*time.Second)).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	return tx.Create(&model.GenerationDispatch{ID: uuid.NewString(), RunID: id, NextAttemptAt: now}).Error
}
func (r *ArtifactRepository) Recover(ctx context.Context) error {
	now := time.Now().UTC()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []model.AgentRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("subject_kind='generation_request' AND (status='pending' OR (status='running' AND (run_lease_until IS NULL OR run_lease_until<=?)))", now).Limit(100).Find(&rows).Error; err != nil {
			return err
		}
		for i := range rows {
			run := &rows[i]
			if run.CancelRequestedAt != nil {
				if err := terminal(tx, run, "cancelled", ""); err != nil {
					return err
				}
				continue
			}
			if run.ExecutionStartedAt != nil && now.Sub(*run.ExecutionStartedAt).Milliseconds() >= run.MaxDurationMs {
				if err := terminal(tx, run, "budget_exhausted", "budget_exhausted"); err != nil {
					return err
				}
				continue
			}
			if run.ExecutionStartedAt == nil {
				var req model.GenerationRequest
				if err := tx.Where("run_id=?", run.ID).First(&req).Error; err != nil {
					return err
				}
				if now.After(req.QueueDeadline) {
					if err := terminal(tx, run, "failed", "queue_expired"); err != nil {
						return err
					}
					continue
				}
			}
			if err := queueDispatch(tx, run.ID, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// Terminal checkpoints are private recovery material, not permanent product versions.
func (r *ArtifactRepository) Prune(ctx context.Context) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		finished := tx.Model(&model.AgentRun{}).Select("id").Where("subject_kind='generation_request' AND status NOT IN ('pending','running') AND finished_at<?", time.Now().UTC().Add(-7*24*time.Hour))
		if err := tx.Model(&model.AgentStep{}).Where("run_id IN (?) AND result_checkpoint<>''", finished).Update("result_checkpoint", "").Error; err != nil {
			return err
		}
		return tx.Where("run_id IN (?)", finished).Delete(&model.GenerationDispatch{}).Error
	})
}
func (r *ArtifactRepository) Dispatches(ctx context.Context) ([]model.GenerationDispatch, error) {
	rows := []model.GenerationDispatch{}
	now := time.Now().UTC()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("published_at IS NULL AND next_attempt_at<=? AND (lease_until IS NULL OR lease_until<=?)", now, now).Order("created_at").Limit(30).Find(&rows).Error; err != nil {
			return err
		}
		for i := range rows {
			rows[i].LeaseToken = uuid.NewString()
			until := now.Add(30 * time.Second)
			rows[i].LeaseUntil = &until
			if err := tx.Model(&rows[i]).Updates(map[string]any{"lease_token": rows[i].LeaseToken, "lease_until": until}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return rows, err
}
func (r *ArtifactRepository) DispatchResult(ctx context.Context, d model.GenerationDispatch, ok bool) error {
	values := map[string]any{"lease_until": nil, "next_attempt_at": time.Now().UTC().Add(5 * time.Second)}
	if ok {
		values["published_at"] = time.Now().UTC()
	}
	return r.db.WithContext(ctx).Model(&model.GenerationDispatch{}).Where("id=? AND lease_token=?", d.ID, d.LeaseToken).Updates(values).Error
}
func (r *ArtifactRepository) Events(ctx context.Context, owner int64, id string, after int64) ([]model.RunEvent, error) {
	run, _, err := r.Run(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	if after < 0 || after > run.EventSeq {
		return nil, artifact.Err("invalid_cursor", 400)
	}
	rows := []model.RunEvent{}
	err = r.db.WithContext(ctx).Where("run_id=? AND seq>?", id, after).Order("seq").Limit(100).Find(&rows).Error
	return rows, err
}

func (r *ArtifactRepository) Commit(ctx context.Context, req *model.GenerationRequest, token string, epoch int64, body artifact.Body, read SourceReader) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		m, err := readableManifest(tx, req.UserID, req.ManifestID)
		if err != nil {
			return err
		}
		task, err := sourceLock(tx, req.UserID, m.SourceID)
		if err != nil {
			return err
		}
		if task.Status == model.TaskStatusQueued || task.Status == model.TaskStatusRunning {
			return artifact.Err("source_changed", 409)
		}
		hash, _, err := read(ctx, NewRepositories(tx), req.UserID, m.SourceID)
		if err != nil {
			return err
		}
		if hash != m.ContentHash {
			return artifact.Err("source_changed", 409)
		}
		a, err := ownedArtifact(tx, req.UserID, req.ArtifactID, true)
		if err != nil {
			return err
		}
		run, err := lockedRun(tx, req.RunID)
		if err != nil {
			return err
		}
		if err = fence(run, token, epoch, time.Now().UTC()); err != nil {
			return err
		}
		if run.CancelRequestedAt != nil {
			return terminal(tx, run, "cancelled", "")
		}
		allowed, err := allowedEvidence(tx, m.ID)
		if err != nil {
			return err
		}
		if err = body.Validate(allowed); err != nil {
			return err
		}
		v, err := insertRevision(tx, a, body, m.ID, "generated", req.BaseVersion, &run.ID, a.HeadVersion == req.BaseVersion)
		if err != nil {
			return err
		}
		run.ResultVersionID = &v.ID
		if err = tx.Model(run).Update("result_version_id", v.ID).Error; err != nil {
			return err
		}
		return terminal(tx, run, "completed", "")
	})
}
