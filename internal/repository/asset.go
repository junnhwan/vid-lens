package repository

import (
	"errors"

	"vid-lens/internal/model"

	"gorm.io/gorm"
)

type AssetRepository struct {
	db *gorm.DB
}

func NewAssetRepository(db *gorm.DB) *AssetRepository {
	return &AssetRepository{db: db}
}

func (r *AssetRepository) Create(asset *model.VideoAsset) error {
	return r.db.Create(asset).Error
}

func (r *AssetRepository) FindByMD5(md5 string) (*model.VideoAsset, error) {
	var asset model.VideoAsset
	err := r.db.Where("file_md5 = ?", md5).First(&asset).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &asset, nil
}
