package repository

import (
	"strings"
	"time"

	"gorm.io/gorm"
	"vid-lens/internal/model"
)

type QuotaCompensationRepository struct{ db *gorm.DB }

func NewQuotaCompensationRepository(db *gorm.DB) *QuotaCompensationRepository {
	return &QuotaCompensationRepository{db: db}
}

func (r *QuotaCompensationRepository) ListDueCompensations(now time.Time, limit int) ([]model.QuotaCompensation, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []model.QuotaCompensation
	err := r.db.Where("(status = ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)) OR (status = ? AND lease_expires_at <= ?)", model.CompensationPending, now, model.CompensationProcessing, now).Order("id asc").Limit(limit).Find(&rows).Error
	return rows, err
}
func (r *QuotaCompensationRepository) ClaimCompensation(id int64, token string, now, until time.Time) (bool, error) {
	res := r.db.Model(&model.QuotaCompensation{}).Where("id = ? AND ((status = ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)) OR (status = ? AND lease_expires_at <= ?))", id, model.CompensationPending, now, model.CompensationProcessing, now).Updates(map[string]any{"status": model.CompensationProcessing, "lease_token": token, "lease_expires_at": until, "updated_at": now})
	return res.RowsAffected == 1, res.Error
}
func (r *QuotaCompensationRepository) CompleteCompensation(id int64, token string, now time.Time) error {
	return r.db.Model(&model.QuotaCompensation{}).Where("id = ? AND status = ? AND lease_token = ?", id, model.CompensationProcessing, token).Updates(map[string]any{"status": model.CompensationCompleted, "completed_at": now, "lease_token": "", "lease_expires_at": nil, "last_error": "", "updated_at": now}).Error
}
func (r *QuotaCompensationRepository) RetryCompensation(id int64, token, msg string, next time.Time, maxAttempts int) error {
	var row model.QuotaCompensation
	if err := r.db.First(&row, "id = ? AND status = ? AND lease_token = ?", id, model.CompensationProcessing, token).Error; err != nil {
		return err
	}
	attempts := row.AttemptCount + 1
	status := model.CompensationPending
	var nextPtr *time.Time = &next
	if maxAttempts > 0 && attempts >= maxAttempts {
		status = model.CompensationDead
		nextPtr = nil
	}
	return r.db.Model(&model.QuotaCompensation{}).Where("id = ? AND status = ? AND lease_token = ?", id, model.CompensationProcessing, token).Updates(map[string]any{"status": status, "attempt_count": attempts, "next_attempt_at": nextPtr, "lease_token": "", "lease_expires_at": nil, "last_error": truncateGovernanceError(strings.TrimSpace(msg)), "updated_at": time.Now()}).Error
}
