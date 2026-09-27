package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"vid-lens/internal/model"
)

// VisualProgress is a read-only view of the latest visual processing attempt.
// total_frames is omitted until frame extraction actually determines it.
type VisualProgress struct {
	TaskID             int64      `json:"task_id"`
	AttemptID          string     `json:"attempt_id,omitempty"`
	Status             string     `json:"status"`
	Phase              string     `json:"phase"`
	TotalFrames        *int       `json:"total_frames"`
	ProcessedFrames    int        `json:"processed_frames"`
	FailedFrames       int        `json:"failed_frames"`
	OCRFailedFrames    int        `json:"ocr_failed_frames"`
	VisionFailedFrames int        `json:"vision_failed_frames"`
	ErrorCode          string     `json:"error_code,omitempty"`
	StartedAt          *time.Time `json:"started_at,omitempty"`
	UpdatedAt          *time.Time `json:"updated_at,omitempty"`
}

func (s *MediaService) GetVisualProgress(_ context.Context, userID, taskID int64) (*VisualProgress, error) {
	task, err := s.repo.Task.FindByID(taskID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	if task == nil || task.UserID != userID {
		return nil, ErrTaskNotFound
	}
	result := &VisualProgress{TaskID: taskID, Status: "not_started", Phase: "not_started"}
	if task.VisualDisabled {
		result.Status, result.Phase, result.ErrorCode = model.VisualProgressSkipped, "disabled", "visual_disabled"
		return result, nil
	}
	if s.repo.VisualProgress == nil {
		return result, nil
	}
	row, err := s.repo.VisualProgress.Find(taskID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return result, nil
	}
	// A new dispatch or replacement worker must never inherit the old attempt's
	// counts. The new branch will create its own row after acquiring the lease.
	if task.LastJobType == model.TaskJobTypeTranscribe &&
		(task.Status == model.TaskStatusQueued || task.Status == model.TaskStatusRunning) &&
		task.ProcessingToken != row.AttemptToken {
		result.Status, result.Phase = "waiting_to_start", "waiting_for_worker"
		return result, nil
	}
	key := sha256.Sum256([]byte(row.AttemptToken))
	result.AttemptID = fmt.Sprintf("va_%x", key[:8])
	result.Status, result.Phase = row.Status, row.Phase
	result.ProcessedFrames, result.FailedFrames = row.Processed, row.Failed
	result.OCRFailedFrames, result.VisionFailedFrames = row.OCRFailed, row.VisionFailed
	result.ErrorCode = row.ErrorCode
	result.StartedAt, result.UpdatedAt = &row.StartedAt, &row.UpdatedAt
	if row.TotalKnown {
		total := row.TotalFrames
		result.TotalFrames = &total
	}
	if row.Status == model.VisualProgressRunning || row.Status == model.VisualProgressQueued {
		if task.Status == model.TaskStatusFailed || task.Status == model.TaskStatusDead {
			result.Status = "interrupted"
			result.ErrorCode = "task_failed"
		} else if task.ProcessingToken != row.AttemptToken || task.Status != model.TaskStatusRunning {
			result.Status = "interrupted"
			result.ErrorCode = "attempt_ended"
		} else if task.Status == model.TaskStatusRunning && task.ProcessingToken == row.AttemptToken &&
			(task.LeaseExpiresAt == nil || !task.LeaseExpiresAt.After(time.Now())) {
			result.Status = "interrupted"
			result.ErrorCode = "lease_expired"
		}
	}
	return result, nil
}
