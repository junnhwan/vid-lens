package repository

import (
	"errors"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"vid-lens/internal/model"
)

type KnowledgeBaseRepository struct {
	db *gorm.DB
}

func NewKnowledgeBaseRepository(db *gorm.DB) *KnowledgeBaseRepository {
	return &KnowledgeBaseRepository{db: db}
}

func (r *KnowledgeBaseRepository) Create(knowledgeBase *model.KnowledgeBase) error {
	knowledgeBase.Name = strings.TrimSpace(knowledgeBase.Name)
	return r.db.Create(knowledgeBase).Error
}

func (r *KnowledgeBaseRepository) ListByUserID(userID int64) ([]model.KnowledgeBase, error) {
	var knowledgeBases []model.KnowledgeBase
	err := r.db.Where("user_id = ?", userID).
		Order("updated_at DESC, id DESC").
		Find(&knowledgeBases).Error
	return knowledgeBases, err
}

func (r *KnowledgeBaseRepository) FindByIDForUser(userID, knowledgeBaseID int64) (*model.KnowledgeBase, error) {
	var knowledgeBase model.KnowledgeBase
	err := r.db.Where("user_id = ? AND id = ?", userID, knowledgeBaseID).First(&knowledgeBase).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &knowledgeBase, nil
}

// FindByIDForUserForUpdate lets the service serialize member-limit checks and
// membership changes in the same PostgreSQL transaction.
func (r *KnowledgeBaseRepository) FindByIDForUserForUpdate(userID, knowledgeBaseID int64) (*model.KnowledgeBase, error) {
	var knowledgeBase model.KnowledgeBase
	err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ? AND id = ?", userID, knowledgeBaseID).
		First(&knowledgeBase).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &knowledgeBase, nil
}

func (r *KnowledgeBaseRepository) UpdateForUser(userID int64, knowledgeBase *model.KnowledgeBase) error {
	result := r.db.Model(&model.KnowledgeBase{}).
		Where("user_id = ? AND id = ?", userID, knowledgeBase.ID).
		Updates(map[string]any{
			"name":        strings.TrimSpace(knowledgeBase.Name),
			"description": knowledgeBase.Description,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *KnowledgeBaseRepository) DeleteForUser(userID, knowledgeBaseID int64) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&model.KnowledgeBase{}).
			Where("user_id = ? AND id = ?", userID, knowledgeBaseID).
			Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Where("knowledge_base_id = ?", knowledgeBaseID).
			Delete(&model.KnowledgeBaseVideo{}).Error; err != nil {
			return err
		}
		return tx.Where("user_id = ? AND id = ?", userID, knowledgeBaseID).
			Delete(&model.KnowledgeBase{}).Error
	})
}

// AddVideoForUser verifies both ownership edges and inserts duplicate-safe.
// Readiness and member-count rules intentionally remain in the service layer.
func (r *KnowledgeBaseRepository) AddVideoForUser(userID, knowledgeBaseID, taskID int64) (bool, error) {
	var knowledgeBaseCount int64
	if err := r.db.Model(&model.KnowledgeBase{}).
		Where("id = ? AND user_id = ?", knowledgeBaseID, userID).
		Count(&knowledgeBaseCount).Error; err != nil {
		return false, err
	}
	if knowledgeBaseCount == 0 {
		return false, gorm.ErrRecordNotFound
	}

	var taskCount int64
	if err := r.db.Model(&model.VideoTask{}).
		Where("id = ? AND user_id = ?", taskID, userID).
		Count(&taskCount).Error; err != nil {
		return false, err
	}
	if taskCount == 0 {
		return false, gorm.ErrRecordNotFound
	}

	result := r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "knowledge_base_id"}, {Name: "task_id"}},
		DoNothing: true,
	}).Create(&model.KnowledgeBaseVideo{KnowledgeBaseID: knowledgeBaseID, TaskID: taskID})
	return result.RowsAffected == 1, result.Error
}

func (r *KnowledgeBaseRepository) RemoveVideoForUser(userID, knowledgeBaseID, taskID int64) error {
	var count int64
	if err := r.db.Model(&model.KnowledgeBase{}).
		Where("id = ? AND user_id = ?", knowledgeBaseID, userID).
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	result := r.db.Where("knowledge_base_id = ? AND task_id = ?", knowledgeBaseID, taskID).
		Delete(&model.KnowledgeBaseVideo{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *KnowledgeBaseRepository) ListVideosForUser(userID, knowledgeBaseID int64) ([]model.KnowledgeBaseVideo, error) {
	var videos []model.KnowledgeBaseVideo
	err := r.db.Table("knowledge_base_videos AS kbv").
		Select("kbv.*").
		Joins("JOIN knowledge_bases AS kb ON kb.id = kbv.knowledge_base_id").
		Joins("JOIN video_tasks AS vt ON vt.id = kbv.task_id AND vt.deleted_at IS NULL").
		Where("kb.id = ? AND kb.user_id = ? AND vt.user_id = ?", knowledgeBaseID, userID, userID).
		Order("kbv.task_id ASC, kbv.id ASC").
		Scan(&videos).Error
	return videos, err
}

func (r *KnowledgeBaseRepository) ListMemberTaskIDsForUser(userID, knowledgeBaseID int64) ([]int64, error) {
	var taskIDs []int64
	err := r.db.Table("knowledge_base_videos AS kbv").
		Joins("JOIN knowledge_bases AS kb ON kb.id = kbv.knowledge_base_id").
		Joins("JOIN video_tasks AS vt ON vt.id = kbv.task_id AND vt.deleted_at IS NULL").
		Where("kb.id = ? AND kb.user_id = ? AND vt.user_id = ?", knowledgeBaseID, userID, userID).
		Order("kbv.task_id ASC, kbv.id ASC").
		Pluck("kbv.task_id", &taskIDs).Error
	return taskIDs, err
}

func (r *KnowledgeBaseRepository) CountMembers(knowledgeBaseID int64) (int64, error) {
	var count int64
	err := r.db.Model(&model.KnowledgeBaseVideo{}).
		Where("knowledge_base_id = ?", knowledgeBaseID).
		Count(&count).Error
	return count, err
}

// CountVideos is kept as a descriptive alias for callers that use video terms.
func (r *KnowledgeBaseRepository) CountVideos(knowledgeBaseID int64) (int64, error) {
	return r.CountMembers(knowledgeBaseID)
}

func (r *KnowledgeBaseRepository) DeleteMembershipsByTaskID(taskID int64) error {
	return r.db.Where("task_id = ?", taskID).Delete(&model.KnowledgeBaseVideo{}).Error
}
