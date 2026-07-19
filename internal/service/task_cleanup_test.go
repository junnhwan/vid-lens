package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

func TestDeleteTaskRejectsActiveTask(t *testing.T) {
	for _, status := range []int8{model.TaskStatusQueued, model.TaskStatusRunning} {
		t.Run(taskStatusName(status), func(t *testing.T) {
			repos := newMediaTestRepositories(t)
			asset := createMediaTestAsset(t, repos, "11111111111111111111111111111111", "videos/active-delete.mp4")
			task := createMediaTestTask(t, repos, 7, asset, "active-delete.mp4")
			if err := repos.Task.UpdateStatus(task.ID, status, ""); err != nil {
				t.Fatalf("set active status: %v", err)
			}

			svc := newMediaTestServiceWithCleanup(repos, &recordingObjectStorage{}, nil)
			err := svc.DeleteTask(context.Background(), task.UserID, task.ID)
			if !errors.Is(err, ErrTaskActive) {
				t.Fatalf("DeleteTask() error = %v, want ErrTaskActive", err)
			}
			if _, findErr := repos.Task.FindByID(task.ID); findErr != nil {
				t.Fatalf("active task must remain visible: %v", findErr)
			}
		})
	}
}

func taskStatusName(status int8) string {
	if status == model.TaskStatusQueued {
		return "queued"
	}
	return "running"
}

func TestDeleteTaskReturnsTypedLookupErrors(t *testing.T) {
	repos := newMediaTestRepositories(t)
	asset := createMediaTestAsset(t, repos, "22222222222222222222222222222222", "videos/owned-delete.mp4")
	task := createMediaTestTask(t, repos, 7, asset, "owned-delete.mp4")
	svc := newMediaTestServiceWithCleanup(repos, &recordingObjectStorage{}, nil)

	if err := svc.DeleteTask(context.Background(), task.UserID, task.ID+999); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("missing DeleteTask() error = %v, want ErrTaskNotFound", err)
	}
	if err := svc.DeleteTask(context.Background(), task.UserID+1, task.ID); !errors.Is(err, ErrTaskForbidden) {
		t.Fatalf("foreign DeleteTask() error = %v, want ErrTaskForbidden", err)
	}
}

func TestTaskCleanupRequestCommitsIntentWithTaskSoftDelete(t *testing.T) {
	repos := newMediaTestRepositories(t)
	asset := createMediaTestAsset(t, repos, "33333333333333333333333333333333", "videos/durable-intent.mp4")
	task := createMediaTestTask(t, repos, 7, asset, "durable-intent.mp4")
	createTaskOwnedData(t, repos, task.ID, task.UserID, "embed-v1")
	cleanup := NewTaskCleanupService(repos, nil, nil, TaskCleanupConfig{})

	job, err := cleanup.RequestDelete(context.Background(), task.UserID, task.ID)
	if err != nil {
		t.Fatalf("RequestDelete() error = %v", err)
	}
	if _, err := repos.Task.FindByID(task.ID); err == nil {
		t.Fatal("task must be hidden after cleanup intent commits")
	}
	if job.TaskID != task.ID || job.UserID != task.UserID || job.AssetID == nil || *job.AssetID != asset.ID || job.ObjectName != asset.ObjectName || job.FileMD5 != asset.FileMD5 {
		t.Fatalf("cleanup snapshot = %+v", job)
	}
	stored, err := repos.TaskCleanup.FindByTaskID(task.ID)
	if err != nil || stored == nil || stored.ID != job.ID || stored.Status != model.TaskCleanupStatusPending {
		t.Fatalf("stored cleanup job = %+v, %v", stored, err)
	}
	// Recovery facts remain until external cleanup has succeeded.
	models, err := repos.VideoChunk.ListEmbeddingModelsByTask(task.UserID, task.ID)
	if err != nil || len(models) != 1 || models[0] != "embed-v1" {
		t.Fatalf("embedding recovery facts = %v, %v", models, err)
	}

	repeated, err := cleanup.RequestDelete(context.Background(), task.UserID, task.ID)
	if err != nil || repeated.ID != job.ID {
		t.Fatalf("repeated RequestDelete() = %+v, %v, want existing job", repeated, err)
	}
}

func TestTaskCleanupRequestRemovesKnowledgeBaseMembershipWithTaskSoftDelete(t *testing.T) {
	repos, db := newMediaTestRepositoriesAndDB(t)
	asset := createMediaTestAsset(t, repos, "33333333333333333333333333333334", "videos/kb-membership-delete.mp4")
	task := createMediaTestTask(t, repos, 7, asset, "kb-membership-delete.mp4")
	kb := &model.KnowledgeBase{UserID: task.UserID, Name: "cleanup-kb"}
	if err := repos.KnowledgeBase.Create(kb); err != nil {
		t.Fatalf("create KB: %v", err)
	}
	if _, err := repos.KnowledgeBase.AddVideoForUser(task.UserID, kb.ID, task.ID); err != nil {
		t.Fatalf("add KB membership: %v", err)
	}
	cleanup := NewTaskCleanupService(repos, nil, nil, TaskCleanupConfig{})
	if _, err := cleanup.RequestDelete(context.Background(), task.UserID, task.ID); err != nil {
		t.Fatalf("RequestDelete() error = %v", err)
	}
	var membershipCount int64
	if err := db.Model(&model.KnowledgeBaseVideo{}).Where("knowledge_base_id = ? AND task_id = ?", kb.ID, task.ID).Count(&membershipCount).Error; err != nil {
		t.Fatalf("count KB memberships: %v", err)
	}
	if membershipCount != 0 {
		t.Fatalf("membership count after soft-delete transaction = %d, want 0", membershipCount)
	}
	if _, err := repos.Task.FindByID(task.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("task lookup after RequestDelete() error = %v, want gorm.ErrRecordNotFound", err)
	}
}

func TestTaskCleanupRetriesVectorFailureFromPersistedFacts(t *testing.T) {
	repos := newMediaTestRepositories(t)
	storage := &recordingObjectStorage{}
	cleaner := &recordingTaskVectorCleaner{err: errors.New("pgvector unavailable")}
	asset := createMediaTestAsset(t, repos, "44444444444444444444444444444444", "videos/vector-retry.mp4")
	task := createMediaTestTask(t, repos, 7, asset, "vector-retry.mp4")
	createTaskOwnedData(t, repos, task.ID, task.UserID, "embed-v1")
	now := time.Date(2026, 7, 17, 19, 0, 0, 0, time.UTC)
	token := 0
	cleanup := NewTaskCleanupService(repos, storage, cleaner, TaskCleanupConfig{
		LeaseDuration: time.Minute,
		RetryBackoff:  time.Minute,
		Now:           func() time.Time { return now },
		NewToken: func() string {
			token++
			return fmt.Sprintf("owner-%d", token)
		},
	})
	job, err := cleanup.RequestDelete(context.Background(), task.UserID, task.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := cleanup.ExecuteJob(context.Background(), job.ID); err == nil {
		t.Fatal("ExecuteJob() should report vector cleanup failure")
	}
	stored, err := repos.TaskCleanup.FindByTaskID(task.ID)
	if err != nil || stored.Status != model.TaskCleanupStatusFailed || stored.NextRetryAt == nil {
		t.Fatalf("failed cleanup job = %+v, %v", stored, err)
	}
	models, err := repos.VideoChunk.ListEmbeddingModelsByTask(task.UserID, task.ID)
	if err != nil || len(models) != 1 {
		t.Fatalf("vector recovery facts must remain: %v, %v", models, err)
	}
	if len(storage.deleted) != 0 {
		t.Fatalf("object deleted before vector cleanup: %v", storage.deleted)
	}

	cleaner.err = nil
	now = now.Add(time.Minute)
	if err := cleanup.ExecuteJob(context.Background(), job.ID); err != nil {
		t.Fatalf("retry ExecuteJob() error = %v", err)
	}
	stored, err = repos.TaskCleanup.FindByTaskID(task.ID)
	if err != nil || stored.Status != model.TaskCleanupStatusCompleted || stored.CompletedAt == nil {
		t.Fatalf("completed cleanup job = %+v, %v", stored, err)
	}
	assertTaskOwnedDataDeleted(t, repos, task.ID, task.UserID, "embed-v1")
	if len(storage.deleted) != 1 || storage.deleted[0] != asset.ObjectName {
		t.Fatalf("deleted objects = %v, want %s", storage.deleted, asset.ObjectName)
	}
}

type flakyCleanupObjectStorage struct {
	deleted []string
	err     error
}

func (s *flakyCleanupObjectStorage) DeleteObject(_ context.Context, objectName string) error {
	s.deleted = append(s.deleted, objectName)
	return s.err
}

func TestTaskCleanupReservesAssetAcrossMinIOFailure(t *testing.T) {
	repos := newMediaTestRepositories(t)
	storage := &flakyCleanupObjectStorage{err: errors.New("minio unavailable")}
	asset := createMediaTestAsset(t, repos, "55555555555555555555555555555555", "videos/object-retry.mp4")
	task := createMediaTestTask(t, repos, 7, asset, "object-retry.mp4")
	now := time.Date(2026, 7, 17, 20, 0, 0, 0, time.UTC)
	cleanup := NewTaskCleanupService(repos, storage, nil, TaskCleanupConfig{
		LeaseDuration: time.Minute,
		RetryBackoff:  time.Minute,
		Now:           func() time.Time { return now },
		NewToken:      func() string { return fmt.Sprintf("lease-%d", now.Unix()) },
	})
	job, err := cleanup.RequestDelete(context.Background(), task.UserID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanup.ExecuteJob(context.Background(), job.ID); err == nil {
		t.Fatal("ExecuteJob() should report MinIO failure")
	}

	reserved, err := repos.Asset.FindByIDUnscoped(asset.ID)
	if err != nil || reserved.LifecycleState != model.AssetLifecycleDeleting || reserved.DeleteOwnerJobID == nil || *reserved.DeleteOwnerJobID != job.ID {
		t.Fatalf("reserved asset = %+v, %v", reserved, err)
	}
	if found, err := repos.Asset.FindByMD5(asset.FileMD5); err != nil || found != nil {
		t.Fatalf("deleting asset must not be reusable, got %+v, %v", found, err)
	}
	media := &MediaService{repo: repos}
	if _, err := media.createTaskFromAsset(9, "stale.mp4", asset, model.TaskStatusPending); !errors.Is(err, repository.ErrAssetNotActive) {
		t.Fatalf("createTaskFromAsset() error = %v, want ErrAssetNotActive", err)
	}

	storage.err = nil
	now = now.Add(time.Minute)
	if err := cleanup.ExecuteJob(context.Background(), job.ID); err != nil {
		t.Fatalf("retry ExecuteJob() error = %v", err)
	}
	stored, err := repos.TaskCleanup.FindByTaskID(task.ID)
	if err != nil || stored.Status != model.TaskCleanupStatusCompleted {
		t.Fatalf("completed job = %+v, %v", stored, err)
	}
	if found, err := repos.Asset.FindByMD5(asset.FileMD5); err != nil || found != nil {
		t.Fatalf("completed cleanup asset = %+v, %v, want deleted", found, err)
	}
}

func TestMediaDeleteTaskRequiresInjectedCleanupService(t *testing.T) {
	repos := newMediaTestRepositories(t)
	asset := createMediaTestAsset(t, repos, "ddddddddddddddddddddddddddddddde", "videos/missing-cleanup.mp4")
	task := createMediaTestTask(t, repos, 7, asset, "missing-cleanup.mp4")
	media := &MediaService{repo: repos}

	if err := media.DeleteTask(context.Background(), task.UserID, task.ID); !errors.Is(err, ErrTaskCleanupUnavailable) {
		t.Fatalf("DeleteTask() error = %v, want ErrTaskCleanupUnavailable", err)
	}
	if found, err := repos.Task.FindByID(task.ID); err != nil || found == nil {
		t.Fatalf("task must remain visible when cleanup is not wired: %+v, %v", found, err)
	}
}

func TestMediaDeleteTaskReturnsSuccessAfterDurableIntentWhenImmediateCleanupFails(t *testing.T) {
	repos := newMediaTestRepositories(t)
	storage := &flakyCleanupObjectStorage{err: errors.New("minio unavailable")}
	asset := createMediaTestAsset(t, repos, "66666666666666666666666666666666", "videos/accepted-delete.mp4")
	task := createMediaTestTask(t, repos, 7, asset, "accepted-delete.mp4")
	cleanup := NewTaskCleanupService(repos, storage, nil, TaskCleanupConfig{})
	media := &MediaService{repo: repos}
	media.SetTaskCleanupService(cleanup)

	if err := media.DeleteTask(context.Background(), task.UserID, task.ID); err != nil {
		t.Fatalf("DeleteTask() error = %v; committed cleanup must be asynchronous", err)
	}
	if _, err := repos.Task.FindByID(task.ID); err == nil {
		t.Fatal("task must be hidden after accepted deletion")
	}
	job, err := repos.TaskCleanup.FindByTaskID(task.ID)
	if err != nil || job == nil || job.Status != model.TaskCleanupStatusFailed {
		t.Fatalf("durable failed cleanup = %+v, %v", job, err)
	}
}

func TestTaskCleanupKeepsSharedAssetUntilLastActiveTaskIsDeleted(t *testing.T) {
	repos := newMediaTestRepositories(t)
	storage := &recordingObjectStorage{}
	asset := createMediaTestAsset(t, repos, "77777777777777777777777777777777", "videos/shared.mp4")
	firstTask := createMediaTestTask(t, repos, 7, asset, "shared-first.mp4")
	secondTask := createMediaTestTask(t, repos, 8, asset, "shared-second.mp4")
	cleanup := NewTaskCleanupService(repos, storage, nil, TaskCleanupConfig{})

	firstJob, err := cleanup.RequestDelete(context.Background(), firstTask.UserID, firstTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanup.ExecuteJob(context.Background(), firstJob.ID); err != nil {
		t.Fatalf("first ExecuteJob() error = %v", err)
	}
	if len(storage.deleted) != 0 {
		t.Fatalf("shared object deleted while another task is active: %v", storage.deleted)
	}
	if found, err := repos.Asset.FindByMD5(asset.FileMD5); err != nil || found == nil {
		t.Fatalf("shared asset must remain active: %+v, %v", found, err)
	}

	secondJob, err := cleanup.RequestDelete(context.Background(), secondTask.UserID, secondTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanup.ExecuteJob(context.Background(), secondJob.ID); err != nil {
		t.Fatalf("second ExecuteJob() error = %v", err)
	}
	if len(storage.deleted) != 1 || storage.deleted[0] != asset.ObjectName {
		t.Fatalf("last task cleanup deleted objects = %v, want [%s]", storage.deleted, asset.ObjectName)
	}
	assertCleanupJobStatus(t, repos, firstJob.ID, model.TaskCleanupStatusCompleted)
	assertCleanupJobStatus(t, repos, secondJob.ID, model.TaskCleanupStatusCompleted)
}

func TestTaskCleanupSharedAssetHasSingleDeletionOwnerWhenBothTasksAreHidden(t *testing.T) {
	repos := newMediaTestRepositories(t)
	storage := &recordingObjectStorage{}
	asset := createMediaTestAsset(t, repos, "88888888888888888888888888888888", "videos/shared-hidden.mp4")
	firstTask := createMediaTestTask(t, repos, 7, asset, "shared-hidden-first.mp4")
	secondTask := createMediaTestTask(t, repos, 8, asset, "shared-hidden-second.mp4")
	cleanup := NewTaskCleanupService(repos, storage, nil, TaskCleanupConfig{})

	firstJob, err := cleanup.RequestDelete(context.Background(), firstTask.UserID, firstTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondJob, err := cleanup.RequestDelete(context.Background(), secondTask.UserID, secondTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanup.ExecuteJob(context.Background(), firstJob.ID); err != nil {
		t.Fatalf("first ExecuteJob() error = %v", err)
	}
	if err := cleanup.ExecuteJob(context.Background(), secondJob.ID); err != nil {
		t.Fatalf("second ExecuteJob() error = %v", err)
	}

	if len(storage.deleted) != 1 || storage.deleted[0] != asset.ObjectName {
		t.Fatalf("shared object must have exactly one deletion owner, calls = %v", storage.deleted)
	}
	assertCleanupJobStatus(t, repos, firstJob.ID, model.TaskCleanupStatusCompleted)
	assertCleanupJobStatus(t, repos, secondJob.ID, model.TaskCleanupStatusCompleted)
}

func TestTaskCleanupRetriesDatabaseFinalizationAfterObjectDeletion(t *testing.T) {
	repos, db := newMediaTestRepositoriesAndDB(t)
	storage := &recordingObjectStorage{}
	asset := createMediaTestAsset(t, repos, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaab", "videos/db-retry.mp4")
	task := createMediaTestTask(t, repos, 7, asset, "db-retry.mp4")
	createTaskOwnedData(t, repos, task.ID, task.UserID, "embed-v1")
	now := time.Date(2026, 7, 17, 22, 0, 0, 0, time.UTC)
	cleanup := NewTaskCleanupService(repos, storage, nil, TaskCleanupConfig{
		LeaseDuration: time.Minute,
		RetryBackoff:  time.Minute,
		Now:           func() time.Time { return now },
		NewToken:      func() string { return fmt.Sprintf("db-lease-%d", now.Unix()) },
	})
	job, err := cleanup.RequestDelete(context.Background(), task.UserID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_cleanup_finalize BEFORE DELETE ON video_transcriptions BEGIN SELECT RAISE(ABORT, 'forced finalization failure'); END`).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	if err := cleanup.ExecuteJob(context.Background(), job.ID); err == nil {
		t.Fatal("ExecuteJob() should report database finalization failure")
	}
	assertCleanupJobStatus(t, repos, job.ID, model.TaskCleanupStatusFailed)
	if len(storage.deleted) != 1 {
		t.Fatalf("object deletion calls = %v, want one", storage.deleted)
	}
	reserved, err := repos.Asset.FindByIDUnscoped(asset.ID)
	if err != nil || reserved.LifecycleState != model.AssetLifecycleDeleting {
		t.Fatalf("asset after DB failure = %+v, %v", reserved, err)
	}
	if transcription, err := repos.Transcription.FindByTaskID(task.ID); err != nil || transcription == nil {
		t.Fatalf("transaction rollback must retain task-owned rows: %+v, %v", transcription, err)
	}

	if err := db.Exec(`DROP TRIGGER fail_cleanup_finalize`).Error; err != nil {
		t.Fatalf("drop failure trigger: %v", err)
	}
	now = now.Add(time.Minute)
	if err := cleanup.ExecuteJob(context.Background(), job.ID); err != nil {
		t.Fatalf("retry ExecuteJob() error = %v", err)
	}
	if len(storage.deleted) != 2 {
		t.Fatalf("idempotent object deletion calls after retry = %v, want two", storage.deleted)
	}
	assertCleanupJobStatus(t, repos, job.ID, model.TaskCleanupStatusCompleted)
	assertTaskOwnedDataDeleted(t, repos, task.ID, task.UserID, "embed-v1")
}

func TestTaskCleanupRequestRollsBackWhenIntentInsertFails(t *testing.T) {
	repos, db := newMediaTestRepositoriesAndDB(t)
	asset := createMediaTestAsset(t, repos, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbc", "videos/intent-rollback.mp4")
	task := createMediaTestTask(t, repos, 7, asset, "intent-rollback.mp4")
	if err := db.Exec(`CREATE TRIGGER fail_cleanup_intent BEFORE INSERT ON task_cleanup_jobs BEGIN SELECT RAISE(ABORT, 'forced intent failure'); END`).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	kb := &model.KnowledgeBase{UserID: task.UserID, Name: "rollback-kb"}
	if err := repos.KnowledgeBase.Create(kb); err != nil {
		t.Fatalf("create rollback KB: %v", err)
	}
	if _, err := repos.KnowledgeBase.AddVideoForUser(task.UserID, kb.ID, task.ID); err != nil {
		t.Fatalf("add rollback membership: %v", err)
	}
	cleanup := NewTaskCleanupService(repos, nil, nil, TaskCleanupConfig{})

	if _, err := cleanup.RequestDelete(context.Background(), task.UserID, task.ID); err == nil {
		t.Fatal("RequestDelete() should report cleanup intent insert failure")
	}
	if found, err := repos.Task.FindByID(task.ID); err != nil || found == nil {
		t.Fatalf("task must remain visible after intent rollback: %+v, %v", found, err)
	}
	if job, err := repos.TaskCleanup.FindByTaskID(task.ID); err != nil || job != nil {
		t.Fatalf("rolled-back cleanup intent = %+v, %v", job, err)
	}
	var membershipCount int64
	if err := db.Model(&model.KnowledgeBaseVideo{}).Where("knowledge_base_id = ? AND task_id = ?", kb.ID, task.ID).Count(&membershipCount).Error; err != nil {
		t.Fatalf("count rolled-back membership: %v", err)
	}
	if membershipCount != 1 {
		t.Fatalf("membership count after intent rollback = %d, want 1", membershipCount)
	}
}

func assertCleanupJobStatus(t *testing.T, repos *repository.Repositories, jobID int64, want string) {
	t.Helper()
	job, err := repos.TaskCleanup.FindByID(jobID)
	if err != nil || job == nil || job.Status != want {
		t.Fatalf("cleanup job %d = %+v, %v, want status %s", jobID, job, err, want)
	}
}

func newMediaTestRepositoriesAndDB(t *testing.T) (*repository.Repositories, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	return repository.NewRepositories(db), db
}
