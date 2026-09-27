package repository

import (
	"fmt"
	"time"

	"vid-lens/internal/model"

	"gorm.io/gorm"
)

type VideoVisualProgressRepository struct{ db *gorm.DB }

func NewVideoVisualProgressRepository(db *gorm.DB) *VideoVisualProgressRepository {
	return &VideoVisualProgressRepository{db: db}
}

func (r *VideoVisualProgressRepository) Find(taskID int64) (*model.VideoVisualProgress, error) {
	var progress model.VideoVisualProgress
	err := r.db.Where("task_id = ?", taskID).First(&progress).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &progress, nil
}

func (r *VideoVisualProgressRepository) DeleteByTaskID(taskID int64) error {
	return r.db.Where("task_id = ?", taskID).Delete(&model.VideoVisualProgress{}).Error
}

type VisualProgressUpdate struct {
	Phase        string
	Status       string
	TotalKnown   bool
	TotalFrames  int
	Processed    int
	Failed       int
	OCRFailed    int
	VisionFailed int
	ErrorCode    string
}

// BeginVisualProgress starts a new attempt only while the transcribe worker
// owns its processing lease. A duplicate start for the same token is harmless.
func (r *Repositories) BeginVisualProgress(req TaskProcessingLeaseRequest) (bool, error) {
	return r.RunWithTaskProcessingLease(req, func(tx *Repositories) error {
		current, err := tx.VisualProgress.Find(req.TaskID)
		if err != nil {
			return err
		}
		if current != nil && current.AttemptToken == req.Token {
			if visualProgressTerminal(current.Status) {
				return fmt.Errorf("visual progress attempt already complete")
			}
			return nil
		}
		now := req.Now
		if now.IsZero() {
			now = time.Now()
		}
		fresh := model.VideoVisualProgress{
			TaskID: req.TaskID, AttemptToken: req.Token,
			Status: model.VisualProgressQueued, Phase: "waiting_for_slot", StartedAt: now, UpdatedAt: now,
		}
		if current == nil {
			return tx.db.Create(&fresh).Error
		}
		return tx.db.Model(&model.VideoVisualProgress{}).Where("task_id = ?", req.TaskID).Updates(map[string]any{
			"attempt_token": req.Token, "status": fresh.Status, "phase": fresh.Phase,
			"total_known": false, "total_frames": 0, "processed": 0, "failed": 0,
			"ocr_failed": 0, "vision_failed": 0, "error_code": "", "started_at": now, "updated_at": now,
		}).Error
	})
}

// AdvanceVisualProgress serializes progress updates with the task lease row.
// Counters cannot move backwards and terminal updates cannot be reopened.
func (r *Repositories) AdvanceVisualProgress(req TaskProcessingLeaseRequest, update VisualProgressUpdate) (bool, error) {
	return r.RunWithTaskProcessingLease(req, func(tx *Repositories) error {
		current, err := tx.VisualProgress.Find(req.TaskID)
		if err != nil {
			return err
		}
		if current == nil || current.AttemptToken != req.Token {
			return fmt.Errorf("visual progress attempt missing or stale")
		}
		if visualProgressTerminal(current.Status) {
			return nil
		}
		if update.Processed < current.Processed || update.Failed < current.Failed ||
			update.OCRFailed < current.OCRFailed || update.VisionFailed < current.VisionFailed {
			return nil
		}
		if update.Phase != "" && visualProgressPhaseRank(update.Phase) < visualProgressPhaseRank(current.Phase) {
			return nil
		}
		if current.TotalKnown && update.TotalKnown && update.TotalFrames != current.TotalFrames {
			return fmt.Errorf("visual progress total changed within attempt")
		}
		if update.TotalKnown && (update.TotalFrames < 0 || update.Processed > update.TotalFrames) {
			return fmt.Errorf("visual progress counters exceed total")
		}
		if update.Failed > update.Processed || update.OCRFailed > update.Processed || update.VisionFailed > update.Processed {
			return fmt.Errorf("visual progress failed counters exceed processed")
		}
		status := update.Status
		if status == "" {
			status = model.VisualProgressRunning
		}
		phase := update.Phase
		if phase == "" {
			phase = current.Phase
		}
		totalKnown, total := current.TotalKnown, current.TotalFrames
		if update.TotalKnown {
			totalKnown, total = true, update.TotalFrames
		}
		if totalKnown && update.Processed > total {
			return fmt.Errorf("visual progress processed count exceeds known total")
		}
		return tx.db.Model(&model.VideoVisualProgress{}).Where("task_id = ? AND attempt_token = ?", req.TaskID, req.Token).Updates(map[string]any{
			"status": status, "phase": phase, "total_known": totalKnown, "total_frames": total,
			"processed": update.Processed, "failed": update.Failed,
			"ocr_failed": update.OCRFailed, "vision_failed": update.VisionFailed,
			"error_code": update.ErrorCode, "updated_at": req.Now,
		}).Error
	})
}

// PublishVisualFrames atomically swaps stable evidence and marks the attempt
// complete. Reads continue to see the old batch until this transaction commits.
func (r *Repositories) PublishVisualFrames(req TaskProcessingLeaseRequest, frames []model.VideoVisualFrame, update VisualProgressUpdate) (bool, error) {
	return r.RunWithTaskProcessingLease(req, func(tx *Repositories) error {
		current, err := tx.VisualProgress.Find(req.TaskID)
		if err != nil {
			return err
		}
		if current == nil || current.AttemptToken != req.Token || visualProgressTerminal(current.Status) {
			return fmt.Errorf("visual progress attempt missing, stale, or complete")
		}
		if !update.TotalKnown || update.TotalFrames != len(frames) || update.Processed != len(frames) {
			return fmt.Errorf("visual publish requires every extracted frame to finish")
		}
		if err := tx.db.Where("task_id = ?", req.TaskID).Delete(&model.VideoVisualFrame{}).Error; err != nil {
			return err
		}
		if len(frames) > 0 {
			for i := range frames {
				if frames[i].TaskID != req.TaskID {
					return fmt.Errorf("visual frame task mismatch")
				}
			}
			if err := tx.db.Create(&frames).Error; err != nil {
				return err
			}
		}
		return tx.db.Model(&model.VideoVisualProgress{}).Where("task_id = ? AND attempt_token = ?", req.TaskID, req.Token).Updates(map[string]any{
			"status": model.VisualProgressCompleted, "phase": "published", "total_known": true,
			"total_frames": update.TotalFrames, "processed": update.Processed,
			"failed": update.Failed, "ocr_failed": update.OCRFailed, "vision_failed": update.VisionFailed,
			"error_code": "", "updated_at": req.Now,
		}).Error
	})
}

func visualProgressTerminal(status string) bool {
	switch status {
	case model.VisualProgressCompleted, model.VisualProgressSkipped, model.VisualProgressFailed, model.VisualProgressCanceled:
		return true
	default:
		return false
	}
}

func visualProgressPhaseRank(phase string) int {
	switch phase {
	case "waiting_for_slot":
		return 0
	case "provider_check":
		return 1
	case "downloading":
		return 2
	case "extracting":
		return 3
	case "observing_frames":
		return 4
	case "publishing":
		return 5
	case "published":
		return 6
	default:
		return -1
	}
}
