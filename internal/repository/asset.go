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

// CreateOrRestore 创建资产；若相同 MD5 的资产已被软删除，则复用原记录并恢复，
// 避免 unique(file_md5) 与软删除记录冲突。
func (r *AssetRepository) CreateOrRestore(asset *model.VideoAsset) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var existing model.VideoAsset
		err := tx.Unscoped().Where("file_md5 = ?", asset.FileMD5).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Create(asset).Error
		}
		if err != nil {
			return err
		}
		if !existing.DeletedAt.Valid {
			return gorm.ErrDuplicatedKey
		}
		if err := tx.Unscoped().Model(&existing).Updates(map[string]any{
			"object_name":  asset.ObjectName,
			"file_size":    asset.FileSize,
			"content_type": asset.ContentType,
			"deleted_at":   nil,
		}).Error; err != nil {
			return err
		}
		asset.ID = existing.ID
		asset.CreatedAt = existing.CreatedAt
		asset.DeletedAt = gorm.DeletedAt{}
		return nil
	})
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

func (r *AssetRepository) Delete(id int64) error {
	return r.db.Delete(&model.VideoAsset{}, id).Error
}
