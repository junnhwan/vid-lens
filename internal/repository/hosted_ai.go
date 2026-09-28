package repository

import (
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"vid-lens/internal/model"
)

func (r *AIProfileRepository) HostedConfig() (*model.HostedAIConfig, error) {
	var row model.HostedAIConfig
	err := r.db.First(&row, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}

func (r *AIProfileRepository) SaveHostedConfig(row *model.HostedAIConfig) error {
	row.ID = 1
	return r.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"enabled", "ciphertext", "updated_at"})}).Create(row).Error
}

func (r *AIProfileRepository) CanManageHosted(userID int64) bool {
	var count int64
	return userID > 0 && r.db.Model(&model.User{}).Where("id = ? AND role <> ?", userID, model.RoleDemo).Count(&count).Error == nil && count == 1
}

func (r *AIProfileRepository) ActivateHosted(userID int64) (*model.UserAIProfile, error) {
	var profile model.UserAIProfile
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, userID).Error; err != nil {
			return err
		}
		if user.Role == model.RoleDemo {
			return errors.New("演示账号不可修改 AI 配置")
		}
		profile = model.UserAIProfile{UserID: userID, Source: "hosted", Name: "作者免费 AI"}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&profile).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ? AND source = ?", userID, "hosted").First(&profile).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.UserAIProfile{}).Where("user_id = ?", userID).Update("is_default", false).Error; err != nil {
			return err
		}
		profile.IsDefault = true
		return tx.Model(&profile).Update("is_default", true).Error
	})
	return &profile, err
}
