package repository

import (
	"context"
	"vid-lens/internal/model"

	"gorm.io/gorm"
)

func (r *UserRepository) RerankEnabled(ctx context.Context, id int64) (bool, error) {
	var user model.User
	err := r.db.WithContext(ctx).Select("id", "rerank_enabled").Where("id = ?", id).Limit(1).Find(&user).Error
	return user.RerankEnabled, err
}

func (r *UserRepository) SetRerankEnabled(ctx context.Context, id int64, enabled bool) error {
	result := r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).Update("rerank_enabled", enabled)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

type UserRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

// Create 创建用户
func (r *UserRepository) Create(user *model.User) error {
	return r.db.Create(user).Error
}

// FindByUsername 根据用户名查找
func (r *UserRepository) FindByUsername(username string) (*model.User, error) {
	var user model.User
	err := r.db.Where("username = ?", username).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// FindByID 根据 ID 查找
func (r *UserRepository) FindByID(id int64) (*model.User, error) {
	var user model.User
	err := r.db.First(&user, id).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// UpdateRole updates the role for an existing user.
func (r *UserRepository) UpdateRole(id int64, role string) error {
	return r.db.Model(&model.User{}).Where("id = ?", id).Update("role", role).Error
}
