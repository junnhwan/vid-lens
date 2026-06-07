package repository

import (
	"testing"

	"vid-lens/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newTestRepositories(t *testing.T) *Repositories {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}

	return NewRepositories(db)
}

func TestVideoAssetCanBackMultipleUserTasksWithSameMD5(t *testing.T) {
	repos := newTestRepositories(t)

	asset := &model.VideoAsset{
		FileMD5:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ObjectName:  "videos/shared.mp4",
		FileSize:    1024,
		ContentType: "video/mp4",
	}
	if err := repos.Asset.Create(asset); err != nil {
		t.Fatalf("create asset: %v", err)
	}

	first := &model.VideoTask{
		UserID:   1,
		AssetID:  asset.ID,
		FileMD5:  asset.FileMD5,
		Filename: "first.mp4",
		FileURL:  asset.ObjectName,
		FileSize: asset.FileSize,
		Status:   model.TaskStatusPending,
	}
	second := &model.VideoTask{
		UserID:   2,
		AssetID:  asset.ID,
		FileMD5:  asset.FileMD5,
		Filename: "second.mp4",
		FileURL:  asset.ObjectName,
		FileSize: asset.FileSize,
		Status:   model.TaskStatusPending,
	}

	if err := repos.Task.Create(first); err != nil {
		t.Fatalf("create first task: %v", err)
	}
	if err := repos.Task.Create(second); err != nil {
		t.Fatalf("create second task with same md5: %v", err)
	}

	found, err := repos.Asset.FindByMD5(asset.FileMD5)
	if err != nil {
		t.Fatalf("find asset: %v", err)
	}
	if found == nil || found.ID != asset.ID {
		t.Fatalf("expected shared asset %d, got %#v", asset.ID, found)
	}
}

func TestTaskRepositoryCountActiveByAssetIDIgnoresDeletedTasks(t *testing.T) {
	repos := newTestRepositories(t)

	asset := &model.VideoAsset{
		FileMD5:     "cccccccccccccccccccccccccccccccc",
		ObjectName:  "videos/shared-count.mp4",
		FileSize:    2048,
		ContentType: "video/mp4",
	}
	if err := repos.Asset.Create(asset); err != nil {
		t.Fatalf("create asset: %v", err)
	}

	first := &model.VideoTask{
		UserID:   1,
		AssetID:  asset.ID,
		FileMD5:  asset.FileMD5,
		Filename: "first.mp4",
		FileURL:  asset.ObjectName,
		FileSize: asset.FileSize,
		Status:   model.TaskStatusPending,
	}
	second := &model.VideoTask{
		UserID:   2,
		AssetID:  asset.ID,
		FileMD5:  asset.FileMD5,
		Filename: "second.mp4",
		FileURL:  asset.ObjectName,
		FileSize: asset.FileSize,
		Status:   model.TaskStatusPending,
	}
	if err := repos.Task.Create(first); err != nil {
		t.Fatalf("create first task: %v", err)
	}
	if err := repos.Task.Create(second); err != nil {
		t.Fatalf("create second task: %v", err)
	}

	count, err := repos.Task.CountActiveByAssetID(asset.ID)
	if err != nil {
		t.Fatalf("CountActiveByAssetID() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("active count = %d, want 2", count)
	}

	if err := repos.Task.Delete(first.ID); err != nil {
		t.Fatalf("delete first task: %v", err)
	}
	count, err = repos.Task.CountActiveByAssetID(asset.ID)
	if err != nil {
		t.Fatalf("CountActiveByAssetID() after delete error = %v", err)
	}
	if count != 1 {
		t.Fatalf("active count after delete = %d, want 1", count)
	}
}

func TestUpdateStatusIfOnlyTransitionsFromAllowedStatus(t *testing.T) {
	repos := newTestRepositories(t)
	task := &model.VideoTask{
		UserID:   1,
		FileMD5:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Filename: "video.mp4",
		FileURL:  "videos/video.mp4",
		Status:   model.TaskStatusPending,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	updated, err := repos.Task.UpdateStatusIf(task.ID, []int8{model.TaskStatusQueued}, model.TaskStatusRunning, "")
	if err != nil {
		t.Fatalf("unexpected update error: %v", err)
	}
	if updated {
		t.Fatalf("status update should not happen from a disallowed state")
	}

	current, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if current.Status != model.TaskStatusPending {
		t.Fatalf("expected status to stay pending, got %d", current.Status)
	}

	updated, err = repos.Task.UpdateStatusIf(task.ID, []int8{model.TaskStatusPending}, model.TaskStatusQueued, "")
	if err != nil {
		t.Fatalf("allowed update failed: %v", err)
	}
	if !updated {
		t.Fatalf("status update should happen from an allowed state")
	}
}
