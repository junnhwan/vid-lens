package repository

import (
	"errors"
	"time"

	"vid-lens/internal/model"

	"gorm.io/gorm"
)

// UploadSessionRepository persists upload manifests, accepted chunk identities,
// and compare-and-set lifecycle transitions. Object-store work belongs to the
// service layer, not this repository.
type UploadSessionRepository struct {
	db *gorm.DB
}

type UploadSessionClaimRequest struct {
	SessionID  string
	UserID     int64
	Token      string
	Now        time.Time
	LeaseUntil time.Time
}

type UploadSessionCompletion struct {
	SessionID       string
	UserID          int64
	Token           string
	VerifiedMD5     string
	FinalObjectName string
	AssetID         int64
	TaskID          int64
	CompletedAt     time.Time
}

func NewUploadSessionRepository(db *gorm.DB) *UploadSessionRepository {
	return &UploadSessionRepository{db: db}
}

func (r *UploadSessionRepository) Create(session *model.UploadSession) error {
	return r.db.Create(session).Error
}

func (r *UploadSessionRepository) FindByIDForUser(sessionID string, userID int64) (*model.UploadSession, error) {
	var session model.UploadSession
	err := r.db.Where("id = ? AND user_id = ?", sessionID, userID).First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *UploadSessionRepository) FindByActiveKey(userID int64, activeKey string) (*model.UploadSession, error) {
	var session model.UploadSession
	err := r.db.Where(
		"user_id = ? AND active_key = ? AND status IN ?",
		userID, activeKey, []string{model.UploadSessionStatusActive, model.UploadSessionStatusCompleting},
	).First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *UploadSessionRepository) CreateChunk(chunk *model.UploadSessionChunk) error {
	return r.db.Create(chunk).Error
}

func (r *UploadSessionRepository) FindChunk(sessionID string, chunkIndex int) (*model.UploadSessionChunk, error) {
	var chunk model.UploadSessionChunk
	err := r.db.Where("session_id = ? AND chunk_index = ?", sessionID, chunkIndex).First(&chunk).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &chunk, nil
}

func (r *UploadSessionRepository) ListChunks(sessionID string) ([]model.UploadSessionChunk, error) {
	var chunks []model.UploadSessionChunk
	err := r.db.Where("session_id = ?", sessionID).Order("chunk_index ASC").Find(&chunks).Error
	return chunks, err
}

func (r *UploadSessionRepository) ClaimCompletion(req UploadSessionClaimRequest) (bool, error) {
	result := r.db.Model(&model.UploadSession{}).
		Where("id = ? AND user_id = ? AND expires_at > ?", req.SessionID, req.UserID, req.Now).
		Where(
			"status = ? OR (status = ? AND (completion_lease_expires_at IS NULL OR completion_lease_expires_at <= ?))",
			model.UploadSessionStatusActive,
			model.UploadSessionStatusCompleting,
			req.Now,
		).
		Updates(map[string]any{
			"status":                      model.UploadSessionStatusCompleting,
			"completion_token":            req.Token,
			"completion_lease_expires_at": req.LeaseUntil,
			"last_error":                  "",
		})
	return result.RowsAffected == 1, result.Error
}

func (r *UploadSessionRepository) ReleaseCompletion(sessionID string, userID int64, token, lastError string) (bool, error) {
	result := r.db.Model(&model.UploadSession{}).
		Where("id = ? AND user_id = ? AND status = ? AND completion_token = ?", sessionID, userID, model.UploadSessionStatusCompleting, token).
		Updates(map[string]any{
			"status":                      model.UploadSessionStatusActive,
			"completion_token":            "",
			"completion_lease_expires_at": nil,
			"last_error":                  lastError,
		})
	return result.RowsAffected == 1, result.Error
}

func (r *UploadSessionRepository) MarkFailed(sessionID string, userID int64, token, lastError string) (bool, error) {
	result := r.db.Model(&model.UploadSession{}).
		Where("id = ? AND user_id = ? AND status = ? AND completion_token = ?", sessionID, userID, model.UploadSessionStatusCompleting, token).
		Updates(map[string]any{
			"status":                      model.UploadSessionStatusFailed,
			"active_key":                  nil,
			"completion_token":            "",
			"completion_lease_expires_at": nil,
			"last_error":                  lastError,
		})
	return result.RowsAffected == 1, result.Error
}

func (r *UploadSessionRepository) MarkExpired(sessionID string, userID int64, now time.Time) (bool, error) {
	result := r.db.Model(&model.UploadSession{}).
		Where("id = ? AND user_id = ? AND expires_at <= ?", sessionID, userID, now).
		Where(
			"status = ? OR (status = ? AND (completion_lease_expires_at IS NULL OR completion_lease_expires_at <= ?))",
			model.UploadSessionStatusActive,
			model.UploadSessionStatusCompleting,
			now,
		).
		Updates(map[string]any{
			"status":                      model.UploadSessionStatusExpired,
			"active_key":                  nil,
			"completion_token":            "",
			"completion_lease_expires_at": nil,
		})
	return result.RowsAffected == 1, result.Error
}

func (r *UploadSessionRepository) MarkCompleted(completion UploadSessionCompletion) (bool, error) {
	result := r.db.Model(&model.UploadSession{}).
		Where(
			"id = ? AND user_id = ? AND status = ? AND completion_token = ?",
			completion.SessionID,
			completion.UserID,
			model.UploadSessionStatusCompleting,
			completion.Token,
		).
		Updates(map[string]any{
			"status":                      model.UploadSessionStatusCompleted,
			"active_key":                  nil,
			"verified_md5":                completion.VerifiedMD5,
			"final_object_name":           completion.FinalObjectName,
			"asset_id":                    completion.AssetID,
			"task_id":                     completion.TaskID,
			"completion_token":            "",
			"completion_lease_expires_at": nil,
			"last_error":                  "",
			"completed_at":                completion.CompletedAt,
		})
	return result.RowsAffected == 1, result.Error
}
