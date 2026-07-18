package repository

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"vid-lens/internal/model"
)

type TaskCleanupJobRepository struct {
	db *gorm.DB
}

type TaskCleanupClaimRequest struct {
	JobID      int64
	Token      string
	Now        time.Time
	LeaseUntil time.Time
}

func NewTaskCleanupJobRepository(db *gorm.DB) *TaskCleanupJobRepository {
	return &TaskCleanupJobRepository{db: db}
}

func (r *TaskCleanupJobRepository) Create(job *model.TaskCleanupJob) error {
	return r.db.Create(job).Error
}

func (r *TaskCleanupJobRepository) FindByID(jobID int64) (*model.TaskCleanupJob, error) {
	var job model.TaskCleanupJob
	err := r.db.First(&job, jobID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *TaskCleanupJobRepository) FindByTaskID(taskID int64) (*model.TaskCleanupJob, error) {
	var job model.TaskCleanupJob
	err := r.db.Where("task_id = ?", taskID).First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *TaskCleanupJobRepository) FindDue(now time.Time, limit int) ([]model.TaskCleanupJob, error) {
	if limit <= 0 {
		return nil, nil
	}
	var jobs []model.TaskCleanupJob
	err := r.db.Where(
		"(status IN ? AND (next_retry_at IS NULL OR next_retry_at <= ?)) OR (status = ? AND lease_expires_at <= ?)",
		[]string{model.TaskCleanupStatusPending, model.TaskCleanupStatusFailed}, now,
		model.TaskCleanupStatusRunning, now,
	).
		Order("created_at ASC, id ASC").
		Limit(limit).
		Find(&jobs).Error
	return jobs, err
}

func (r *TaskCleanupJobRepository) Claim(req TaskCleanupClaimRequest) (bool, error) {
	result := r.db.Model(&model.TaskCleanupJob{}).
		Where("id = ?", req.JobID).
		Where(
			"(status IN ? AND (next_retry_at IS NULL OR next_retry_at <= ?)) OR (status = ? AND lease_expires_at <= ?)",
			[]string{model.TaskCleanupStatusPending, model.TaskCleanupStatusFailed}, req.Now,
			model.TaskCleanupStatusRunning, req.Now,
		).
		Updates(map[string]any{
			"status":           model.TaskCleanupStatusRunning,
			"attempts":         gorm.Expr("attempts + 1"),
			"next_retry_at":    nil,
			"lease_token":      req.Token,
			"lease_expires_at": req.LeaseUntil,
		})
	return result.RowsAffected == 1, result.Error
}

func (r *TaskCleanupJobRepository) MarkFailed(jobID int64, token, message string, nextRetryAt time.Time) (bool, error) {
	result := r.db.Model(&model.TaskCleanupJob{}).
		Where("id = ? AND status = ? AND lease_token = ?", jobID, model.TaskCleanupStatusRunning, token).
		Updates(map[string]any{
			"status":           model.TaskCleanupStatusFailed,
			"last_error":       message,
			"next_retry_at":    nextRetryAt,
			"lease_token":      "",
			"lease_expires_at": nil,
		})
	return result.RowsAffected == 1, result.Error
}

func (r *TaskCleanupJobRepository) MarkCompleted(jobID int64, token string, completedAt time.Time) (bool, error) {
	result := r.db.Model(&model.TaskCleanupJob{}).
		Where("id = ? AND status = ? AND lease_token = ?", jobID, model.TaskCleanupStatusRunning, token).
		Updates(map[string]any{
			"status":           model.TaskCleanupStatusCompleted,
			"last_error":       "",
			"next_retry_at":    nil,
			"lease_token":      "",
			"lease_expires_at": nil,
			"completed_at":     completedAt,
		})
	return result.RowsAffected == 1, result.Error
}
