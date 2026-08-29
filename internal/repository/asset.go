package repository

import (
	"errors"

	"vid-lens/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrAssetNotActive = errors.New("视频资产正在清理，暂不可复用")

type AssetRepository struct {
	db *gorm.DB
}

func NewAssetRepository(db *gorm.DB) *AssetRepository {
	return &AssetRepository{db: db}
}

func (r *AssetRepository) Create(asset *model.VideoAsset) error {
	return r.db.Create(asset).Error
}

// CreateOrRestore creates an asset or restores a fully deleted record. An asset
// in deleting state is owned by a durable cleanup job and must not be revived.
func (r *AssetRepository) CreateOrRestore(asset *model.VideoAsset) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var existing model.VideoAsset
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Unscoped().
			Where("file_md5 = ?", asset.FileMD5).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Create(asset).Error
		}
		if err != nil {
			return err
		}
		if !existing.DeletedAt.Valid {
			if existing.LifecycleState == model.AssetLifecycleDeleting {
				return ErrAssetNotActive
			}
			return gorm.ErrDuplicatedKey
		}
		if err := tx.Unscoped().Model(&existing).Updates(map[string]any{
			"object_name":         asset.ObjectName,
			"file_size":           asset.FileSize,
			"content_type":        asset.ContentType,
			"lifecycle_state":     model.AssetLifecycleActive,
			"delete_owner_job_id": nil,
			"deleted_at":          nil,
		}).Error; err != nil {
			return err
		}
		asset.ID = existing.ID
		asset.CreatedAt = existing.CreatedAt
		asset.LifecycleState = model.AssetLifecycleActive
		asset.DeleteOwnerJobID = nil
		asset.DeletedAt = gorm.DeletedAt{}
		return nil
	})
}

func (r *AssetRepository) FindByMD5(md5 string) (*model.VideoAsset, error) {
	var asset model.VideoAsset
	err := r.db.Where("file_md5 = ? AND lifecycle_state = ?", md5, model.AssetLifecycleActive).First(&asset).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &asset, nil
}

func (r *AssetRepository) FindByIDUnscoped(id int64) (*model.VideoAsset, error) {
	var asset model.VideoAsset
	err := r.db.Unscoped().First(&asset, id).Error
	if err != nil {
		return nil, err
	}
	return &asset, nil
}

func (r *AssetRepository) FindByIDForUpdateUnscoped(id int64) (*model.VideoAsset, error) {
	var asset model.VideoAsset
	err := r.db.Unscoped().Clauses(clause.Locking{Strength: "UPDATE"}).First(&asset, id).Error
	if err != nil {
		return nil, err
	}
	return &asset, nil
}

func (r *AssetRepository) FindActiveByIDForUpdate(id int64) (*model.VideoAsset, error) {
	var asset model.VideoAsset
	err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND lifecycle_state = ?", id, model.AssetLifecycleActive).
		First(&asset).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrAssetNotActive
	}
	if err != nil {
		return nil, err
	}
	return &asset, nil
}

func (r *AssetRepository) MarkDeleting(id, ownerJobID int64) (bool, error) {
	result := r.db.Model(&model.VideoAsset{}).
		Where("id = ? AND lifecycle_state = ?", id, model.AssetLifecycleActive).
		Updates(map[string]any{
			"lifecycle_state":     model.AssetLifecycleDeleting,
			"delete_owner_job_id": ownerJobID,
		})
	return result.RowsAffected == 1, result.Error
}

func (r *AssetRepository) DeleteOwned(id, ownerJobID int64) (bool, error) {
	result := r.db.Where("id = ? AND lifecycle_state = ? AND delete_owner_job_id = ?", id, model.AssetLifecycleDeleting, ownerJobID).
		Delete(&model.VideoAsset{})
	return result.RowsAffected == 1, result.Error
}

func (r *AssetRepository) Delete(id int64) error {
	return r.db.Delete(&model.VideoAsset{}, id).Error
}
