package repository

import (
	"errors"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"testing"
	"vid-lens/internal/model"
)

func TestAssetCreateOrRestoreRevivesSoftDeletedMD5(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.VideoAsset{}); err != nil {
		t.Fatal(err)
	}
	repo := NewAssetRepository(db)
	old := &model.VideoAsset{FileMD5: "013d02fcc36587fdaaa7c6a4d8d651e2", ObjectName: "videos/truncated.mp4", FileSize: 24}
	if err := repo.Create(old); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(old.ID); err != nil {
		t.Fatal(err)
	}

	replacement := &model.VideoAsset{FileMD5: old.FileMD5, ObjectName: "videos/full.mp4", FileSize: 117, ContentType: "video/mp4"}
	if err := repo.CreateOrRestore(replacement); err != nil {
		t.Fatalf("CreateOrRestore: %v", err)
	}
	if replacement.ID != old.ID {
		t.Fatalf("restored id=%d want %d", replacement.ID, old.ID)
	}
	got, err := repo.FindByMD5(old.FileMD5)
	if err != nil || got == nil {
		t.Fatalf("FindByMD5: asset=%v err=%v", got, err)
	}
	if got.ObjectName != "videos/full.mp4" || got.FileSize != 117 {
		t.Fatalf("restored asset=%+v", got)
	}
}

func TestAssetCreateOrRestoreRejectsDeletingAsset(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.VideoAsset{}); err != nil {
		t.Fatal(err)
	}
	repo := NewAssetRepository(db)
	asset := &model.VideoAsset{FileMD5: "113d02fcc36587fdaaa7c6a4d8d651e2", ObjectName: "videos/deleting.mp4", LifecycleState: model.AssetLifecycleActive}
	if err := repo.Create(asset); err != nil {
		t.Fatal(err)
	}
	reserved, err := repo.MarkDeleting(asset.ID, 42)
	if err != nil || !reserved {
		t.Fatalf("MarkDeleting() = %v, %v", reserved, err)
	}

	replacement := &model.VideoAsset{FileMD5: asset.FileMD5, ObjectName: "videos/replacement.mp4"}
	if err := repo.CreateOrRestore(replacement); !errors.Is(err, ErrAssetNotActive) {
		t.Fatalf("CreateOrRestore() error = %v, want ErrAssetNotActive", err)
	}
	stored, err := repo.FindByIDUnscoped(asset.ID)
	if err != nil || stored.LifecycleState != model.AssetLifecycleDeleting || stored.ObjectName != asset.ObjectName {
		t.Fatalf("deleting asset changed = %+v, %v", stored, err)
	}
}
