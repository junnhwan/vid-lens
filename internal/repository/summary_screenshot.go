package repository

import (
	"context"
	"github.com/google/uuid"
	"gorm.io/gorm/clause"
	"time"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

type RegisterSummaryScreenshotRequest struct {
	UserID, TaskID                                                           int64
	GenerationID, SourceID, SourceDigest, LeaseToken, BlockID, ObservationID string
	CueIDs                                                                   []string
}

// RegisterSummaryScreenshot binds an inspected saved observation to one real
// source window. Callers cannot register an arbitrary object key or timestamp.
func (r *Repositories) RegisterSummaryScreenshot(ctx context.Context, req RegisterSummaryScreenshotRequest) (*model.SummaryScreenshotRef, error) {
	if req.GenerationID == "" || req.BlockID == "" || len(req.BlockID) > 128 || req.ObservationID == "" || len(req.CueIDs) == 0 || len(req.CueIDs) > 128 {
		return nil, artifact.Err("invalid_screenshot_request", 400)
	}
	var result *model.SummaryScreenshotRef
	owned, err := r.RunWithTaskProcessingLease(TaskProcessingLeaseRequest{TaskID: req.TaskID, JobType: model.TaskJobTypeSummary, Token: req.LeaseToken}, func(tx *Repositories) error {
		if _, err := tx.TextSource.LockSource(ctx, req.UserID, req.TaskID, req.SourceID, req.SourceDigest); err != nil {
			return err
		}
		job, err := tx.TaskJob.FindByTaskAndType(req.TaskID, model.TaskJobTypeSummary)
		if err != nil {
			return err
		}
		if job.GenerationID != req.GenerationID || job.InputSourceID != req.SourceID {
			return artifact.Err("generation_stale", 409)
		}
		source, err := tx.TextSource.Read(ctx, req.UserID, req.TaskID, req.SourceID)
		if err != nil {
			return err
		}
		observation, err := tx.VisualObservation.FindByID(ctx, req.UserID, req.TaskID, req.ObservationID)
		if err != nil {
			return err
		}
		if observation == nil || observation.Status != model.VisualObservationStatusObserved || observation.VideoRevision != source.Identity.MediaFingerprint || observation.ObjectKey == "" || observation.RawResponseHash == "" || observation.StartMS < 0 {
			return artifact.Err("image_not_inspected", 422)
		}
		capture := observation.StartMS
		wanted := map[string]bool{}
		for _, id := range req.CueIDs {
			wanted[id] = true
		}
		within := false
		for _, cue := range source.Cues {
			if wanted[cue.ID] {
				delete(wanted, cue.ID)
				if cue.StartMS != nil && cue.EndMS != nil && capture >= *cue.StartMS && capture < *cue.EndMS {
					within = true
				}
			}
		}
		if len(wanted) > 0 || !within {
			return artifact.Err("image_source_window_mismatch", 422)
		}
		row := model.SummaryScreenshotRef{ID: uuid.NewString(), UserID: req.UserID, TaskID: req.TaskID, GenerationID: req.GenerationID, SourceID: req.SourceID, SourceDigest: req.SourceDigest, MediaRevision: source.Identity.MediaFingerprint, BlockID: req.BlockID, ObservationID: observation.ID, ObjectKey: observation.ObjectKey, CaptureMS: capture, Inspected: true, Status: "ready", CreatedAt: time.Now().UTC()}
		if err := tx.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "generation_id"}, {Name: "block_id"}, {Name: "observation_id"}}, DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
		row = model.SummaryScreenshotRef{}
		if err := tx.db.Where("generation_id = ? AND block_id = ? AND observation_id = ? AND user_id = ? AND task_id = ?", req.GenerationID, req.BlockID, req.ObservationID, req.UserID, req.TaskID).First(&row).Error; err != nil {
			return err
		}
		if row.SourceID != source.ID || row.SourceDigest != source.SourceDigest || row.MediaRevision != source.Identity.MediaFingerprint {
			return artifact.Err("source_changed", 409)
		}
		result = &row
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !owned {
		return nil, artifact.Err("generation_stale", 409)
	}
	return result, nil
}

func (r *Repositories) ReadSummaryScreenshot(ctx context.Context, owner, taskID int64, id string) (*model.SummaryScreenshotRef, error) {
	task, err := summaryTask(r.db.WithContext(ctx), owner, taskID, false)
	if err != nil {
		return nil, err
	}
	var ref model.SummaryScreenshotRef
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ? AND task_id = ? AND status = ? AND inspected = ?", id, owner, taskID, "ready", true).First(&ref).Error; err != nil {
		return nil, hideMissing(err)
	}
	// Historical source documents may retain their registered picture. A media
	// replacement invalidates playback/image access to the former media.
	if ref.MediaRevision != task.FileMD5 {
		return nil, artifact.Err("source_changed", 409)
	}
	source, err := r.TextSource.Read(ctx, owner, taskID, ref.SourceID)
	if err != nil {
		return nil, err
	}
	if source.SourceDigest != ref.SourceDigest || source.Identity.MediaFingerprint != ref.MediaRevision {
		return nil, artifact.Err("source_changed", 409)
	}
	return &ref, nil
}
