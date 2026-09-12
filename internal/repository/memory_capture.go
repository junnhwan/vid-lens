package repository

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
	"vid-lens/internal/model"
)

func (r *ChatRepository) EnableDurableMemoryCapture(enabled bool) { r.durableMemoryCapture = enabled }

func enqueueMemoryCapture(tx *gorm.DB, message *model.ChatMessage) error {
	inputs, err := NewMemoryRepository(tx).ResolveMemoryPolicyInputs(tx.Statement.Context, message.UserID, message.SessionID)
	if err != nil {
		return err
	}
	allowed := inputs.SessionPolicy == model.MemorySessionPolicyEnabled || (inputs.SessionPolicy == model.MemorySessionPolicyInherit && inputs.UserEnabled)
	if !allowed {
		return nil
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.MemoryCaptureJob{MessageID: message.ID, UserID: message.UserID, SessionID: message.SessionID, Status: "pending", AvailableAt: time.Now().UTC()}).Error
}

// ClaimMemoryCapture uses CAS after a non-locking read. Competing workers can
// never own the same lease; expired work is eligible again after a crash.
func (r *MemoryRepository) ClaimMemoryCapture(ctx context.Context, now time.Time) (*model.MemoryCaptureJob, error) {
	// A worker that crashes on every attempt must still stop at the retry cap.
	if err := r.db.WithContext(ctx).Model(&model.MemoryCaptureJob{}).Where("status IN ? AND available_at <= ? AND attempts >= 5", []string{"pending", "processing"}, now).Updates(map[string]any{"status": "failed", "lease_token": ""}).Error; err != nil {
		return nil, err
	}
	var job model.MemoryCaptureJob
	claim := r.db.WithContext(ctx).Where("status IN ? AND available_at <= ?", []string{"pending", "processing"}, now).Order("available_at ASC, message_id ASC").Limit(1).Find(&job)
	err := claim.Error
	if err == nil && claim.RowsAffected == 0 {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	token := uuid.NewString()
	result := r.db.WithContext(ctx).Model(&model.MemoryCaptureJob{}).Where("message_id = ? AND status = ? AND lease_token = ? AND available_at <= ?", job.MessageID, job.Status, job.LeaseToken, now).
		Updates(map[string]any{"status": "processing", "lease_token": token, "available_at": now.Add(time.Minute), "attempts": gorm.Expr("attempts + 1")})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, nil
	}
	job.LeaseToken = token
	job.Attempts++
	return &job, nil
}

type MemoryCaptureStatus struct {
	Pending    int64 `json:"pending"`
	Processing int64 `json:"processing"`
	Failed     int64 `json:"failed"`
}

func (r *MemoryRepository) CaptureStatus(ctx context.Context, userID int64) (MemoryCaptureStatus, error) {
	var rows []struct {
		Status string
		Total  int64
	}
	err := r.db.WithContext(ctx).Model(&model.MemoryCaptureJob{}).Select("status, COUNT(*) AS total").Where("user_id = ? AND status IN ?", userID, []string{"pending", "processing", "failed"}).Group("status").Scan(&rows).Error
	var status MemoryCaptureStatus
	for _, row := range rows {
		switch row.Status {
		case "pending":
			status.Pending = row.Total
		case "processing":
			status.Processing = row.Total
		case "failed":
			status.Failed = row.Total
		}
	}
	return status, err
}

func (r *MemoryRepository) RetryFailedCaptures(ctx context.Context, userID int64) error {
	return r.db.WithContext(ctx).Model(&model.MemoryCaptureJob{}).Where("user_id = ? AND status = ?", userID, "failed").Updates(map[string]any{"status": "pending", "attempts": 0, "available_at": time.Now().UTC(), "lease_token": ""}).Error
}

func (r *MemoryRepository) MemoryCaptureMessage(ctx context.Context, job model.MemoryCaptureJob) (*model.ChatMessage, error) {
	var message model.ChatMessage
	err := r.db.WithContext(ctx).Table("chat_messages AS cm").Select("cm.*").Joins("JOIN chat_sessions AS cs ON cs.id = cm.session_id AND cs.user_id = cm.user_id").
		Where("cm.id = ? AND cm.user_id = ? AND cm.session_id = ? AND cm.role = ?", job.MessageID, job.UserID, job.SessionID, "user").Take(&message).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &message, err
}

func (r *MemoryRepository) FinishMemoryCapture(ctx context.Context, job model.MemoryCaptureJob, success bool) error {
	status := "completed"
	next := time.Now().UTC()
	if !success {
		status = "pending"
		next = next.Add(time.Duration(job.Attempts*job.Attempts) * time.Second)
		if job.Attempts >= 5 {
			status = "failed"
		}
	}
	return r.db.WithContext(ctx).Model(&model.MemoryCaptureJob{}).Where("message_id = ? AND lease_token = ? AND status = ?", job.MessageID, job.LeaseToken, "processing").Updates(map[string]any{"status": status, "available_at": next, "lease_token": ""}).Error
}
