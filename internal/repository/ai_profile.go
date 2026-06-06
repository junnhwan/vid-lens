package repository

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
	"vid-lens/internal/model"
)

type AIProfileRepository struct {
	db *gorm.DB
}

func NewAIProfileRepository(db *gorm.DB) *AIProfileRepository {
	return &AIProfileRepository{db: db}
}

func (r *AIProfileRepository) Create(profile *model.UserAIProfile) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if profile.IsDefault {
			if err := tx.Model(&model.UserAIProfile{}).
				Where("user_id = ?", profile.UserID).
				Update("is_default", false).Error; err != nil {
				return err
			}
		}
		return tx.Create(profile).Error
	})
}

func (r *AIProfileRepository) ListByUserID(userID int64) ([]model.UserAIProfile, error) {
	var profiles []model.UserAIProfile
	err := r.db.Where("user_id = ?", userID).Order("is_default desc, id desc").Find(&profiles).Error
	return profiles, err
}

func (r *AIProfileRepository) FindByIDForUser(userID, id int64) (*model.UserAIProfile, error) {
	var profile model.UserAIProfile
	err := r.db.Where("user_id = ? AND id = ?", userID, id).First(&profile).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

func (r *AIProfileRepository) FindDefaultByUserID(userID int64) (*model.UserAIProfile, error) {
	var profile model.UserAIProfile
	err := r.db.Where("user_id = ? AND is_default = ?", userID, true).First(&profile).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

func (r *AIProfileRepository) UpdateForUser(userID int64, profile *model.UserAIProfile) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var existing model.UserAIProfile
		if err := tx.Where("user_id = ? AND id = ?", userID, profile.ID).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("AI 配置不存在")
			}
			return err
		}

		if profile.IsDefault {
			if err := tx.Model(&model.UserAIProfile{}).
				Where("user_id = ? AND id <> ?", userID, profile.ID).
				Update("is_default", false).Error; err != nil {
				return err
			}
		}

		existing.Name = profile.Name
		existing.LLMProvider = profile.LLMProvider
		existing.LLMBaseURL = profile.LLMBaseURL
		existing.LLMAPIKeyCiphertext = profile.LLMAPIKeyCiphertext
		existing.LLMModel = profile.LLMModel
		existing.ASRProvider = profile.ASRProvider
		existing.ASRBaseURL = profile.ASRBaseURL
		existing.ASRAPIKeyCiphertext = profile.ASRAPIKeyCiphertext
		existing.ASRModel = profile.ASRModel
		existing.EmbeddingProvider = profile.EmbeddingProvider
		existing.EmbeddingEndpoint = profile.EmbeddingEndpoint
		existing.EmbeddingAPIKeyCiphertext = profile.EmbeddingAPIKeyCiphertext
		existing.EmbeddingModel = profile.EmbeddingModel
		existing.EmbeddingDim = profile.EmbeddingDim
		existing.IsDefault = profile.IsDefault

		return tx.Save(&existing).Error
	})
}

func (r *AIProfileRepository) DeleteForUser(userID, id int64) error {
	result := r.db.Where("user_id = ? AND id = ?", userID, id).Delete(&model.UserAIProfile{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("AI 配置不存在")
	}
	return nil
}
