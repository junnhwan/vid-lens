package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

// TaskCleanupConfig owns only resource-cleanup timing. Kafka task retry policy
// is intentionally separate because the two lifecycles have different failure
// semantics.
type TaskCleanupConfig struct {
	LeaseDuration time.Duration
	RetryBackoff  time.Duration
	Now           func() time.Time
	NewToken      func() string
}

// TaskCleanupService persists deletion intent before touching any external
// system. Execution is added below this request boundary so callers can treat a
// committed request as user-visible deletion even when cleanup is retried.
type TaskCleanupService struct {
	repo          *repository.Repositories
	objectDeleter objectDeleter
	vectorCleaner TaskVectorCleaner
	config        TaskCleanupConfig
}

func NewTaskCleanupService(
	repo *repository.Repositories,
	objectDeleter objectDeleter,
	vectorCleaner TaskVectorCleaner,
	config TaskCleanupConfig,
) *TaskCleanupService {
	if config.LeaseDuration <= 0 {
		config.LeaseDuration = 2 * time.Minute
	}
	if config.RetryBackoff <= 0 {
		config.RetryBackoff = time.Minute
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.NewToken == nil {
		config.NewToken = uuid.NewString
	}
	return &TaskCleanupService{
		repo:          repo,
		objectDeleter: objectDeleter,
		vectorCleaner: vectorCleaner,
		config:        config,
	}
}

// RequestDelete atomically records a durable cleanup intent and hides the task.
// Repeating a request after that commit returns the same intent.
func (s *TaskCleanupService) RequestDelete(ctx context.Context, userID, taskID int64) (*model.TaskCleanupJob, error) {
	var requested *model.TaskCleanupJob
	err := s.repo.TransactionContext(ctx, func(txRepos *repository.Repositories) error {
		task, err := txRepos.Task.FindByIDForUpdate(taskID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			existing, findErr := txRepos.TaskCleanup.FindByTaskID(taskID)
			if findErr != nil {
				return findErr
			}
			if existing == nil {
				return ErrTaskNotFound
			}
			if existing.UserID != userID {
				return ErrTaskForbidden
			}
			requested = existing
			return nil
		}
		if err != nil {
			return err
		}
		if task.UserID != userID {
			return ErrTaskForbidden
		}
		if task.Status == model.TaskStatusQueued || task.Status == model.TaskStatusRunning {
			return ErrTaskActive
		}

		job := &model.TaskCleanupJob{
			TaskID:     task.ID,
			UserID:     task.UserID,
			AssetID:    task.AssetID,
			ObjectName: task.FileURL,
			FileMD5:    task.FileMD5,
			Status:     model.TaskCleanupStatusPending,
		}
		if err := txRepos.TaskCleanup.Create(job); err != nil {
			return err
		}
		if err := txRepos.Task.Delete(task.ID); err != nil {
			return err
		}
		requested = job
		return nil
	})
	return requested, err
}

// ExecuteJob claims one due cleanup job and performs its idempotent operations.
// A caller may safely retry after an error; stale lease owners cannot finalize.
func (s *TaskCleanupService) ExecuteJob(ctx context.Context, jobID int64) error {
	now := s.config.Now()
	token := s.config.NewToken()
	claimed, err := s.repo.TaskCleanup.Claim(repository.TaskCleanupClaimRequest{
		JobID: jobID, Token: token, Now: now, LeaseUntil: now.Add(s.config.LeaseDuration),
	})
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	job, err := s.repo.TaskCleanup.FindByID(jobID)
	if err != nil {
		return s.failClaim(jobID, token, err)
	}
	if job == nil {
		return s.failClaim(jobID, token, fmt.Errorf("cleanup job %d disappeared after claim", jobID))
	}
	if err := s.executeClaimed(ctx, job, token); err != nil {
		return s.failClaim(jobID, token, err)
	}
	return nil
}

func (s *TaskCleanupService) failClaim(jobID int64, token string, cause error) error {
	message := cause.Error()
	if len(message) > 1000 {
		message = message[:1000]
	}
	updated, markErr := s.repo.TaskCleanup.MarkFailed(jobID, token, message, s.config.Now().Add(s.config.RetryBackoff))
	if markErr != nil {
		return errors.Join(cause, fmt.Errorf("record cleanup failure: %w", markErr))
	}
	if !updated {
		return errors.Join(cause, fmt.Errorf("cleanup lease changed before failure was recorded"))
	}
	return cause
}

func (s *TaskCleanupService) executeClaimed(ctx context.Context, job *model.TaskCleanupJob, token string) error {
	embeddingModels, err := collectTaskEmbeddingModels(s.repo, job.UserID, job.TaskID)
	if err != nil {
		return fmt.Errorf("collect vector cleanup facts: %w", err)
	}
	if s.vectorCleaner != nil {
		for _, modelName := range embeddingModels {
			if err := s.vectorCleaner.DeleteTaskChunks(ctx, job.UserID, job.TaskID, modelName); err != nil {
				return fmt.Errorf("delete vector projection for model %s: %w", modelName, err)
			}
		}
	}

	ownsAssetDeletion, err := s.reserveAssetDeletion(ctx, job)
	if err != nil {
		return err
	}
	if ownsAssetDeletion && strings.TrimSpace(job.ObjectName) != "" {
		if s.objectDeleter == nil {
			return fmt.Errorf("object storage is unavailable")
		}
		if err := s.objectDeleter.DeleteObject(ctx, job.ObjectName); err != nil {
			return fmt.Errorf("delete object %s: %w", job.ObjectName, err)
		}
	}

	return s.repo.TransactionContext(ctx, func(txRepos *repository.Repositories) error {
		if err := deleteTaskOwnedRows(txRepos, job.TaskID); err != nil {
			return err
		}
		if ownsAssetDeletion {
			deleted, err := txRepos.Asset.DeleteOwned(*job.AssetID, job.ID)
			if err != nil {
				return err
			}
			if !deleted {
				return fmt.Errorf("asset deletion ownership changed before completion")
			}
		}
		completed, err := txRepos.TaskCleanup.MarkCompleted(job.ID, token, s.config.Now())
		if err != nil {
			return err
		}
		if !completed {
			return fmt.Errorf("cleanup lease changed before completion")
		}
		return nil
	})
}

func (s *TaskCleanupService) reserveAssetDeletion(ctx context.Context, job *model.TaskCleanupJob) (bool, error) {
	if job.AssetID == nil || *job.AssetID <= 0 {
		return false, nil
	}
	ownsDeletion := false
	err := s.repo.TransactionContext(ctx, func(txRepos *repository.Repositories) error {
		asset, err := txRepos.Asset.FindByIDForUpdateUnscoped(*job.AssetID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if asset.DeletedAt.Valid {
			return nil
		}
		activeRefs, err := txRepos.Task.CountActiveByAssetID(asset.ID)
		if err != nil {
			return err
		}
		if activeRefs > 0 {
			return nil
		}
		if asset.LifecycleState == model.AssetLifecycleDeleting {
			ownsDeletion = asset.DeleteOwnerJobID != nil && *asset.DeleteOwnerJobID == job.ID
			return nil
		}
		reserved, err := txRepos.Asset.MarkDeleting(asset.ID, job.ID)
		if err != nil {
			return err
		}
		ownsDeletion = reserved
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("reserve asset deletion: %w", err)
	}
	return ownsDeletion, nil
}

func collectTaskEmbeddingModels(repos *repository.Repositories, userID, taskID int64) ([]string, error) {
	seen := make(map[string]bool)
	addModels := func(models []string) {
		for _, modelName := range models {
			modelName = strings.TrimSpace(modelName)
			if modelName != "" {
				seen[modelName] = true
			}
		}
	}
	chunkModels, err := repos.VideoChunk.ListEmbeddingModelsByTask(userID, taskID)
	if err != nil {
		return nil, err
	}
	addModels(chunkModels)
	indexModels, err := repos.RAGIndex.ListEmbeddingModelsByTask(userID, taskID)
	if err != nil {
		return nil, err
	}
	addModels(indexModels)
	models := make([]string, 0, len(seen))
	for modelName := range seen {
		models = append(models, modelName)
	}
	sort.Strings(models)
	return models, nil
}

func deleteTaskOwnedRows(repos *repository.Repositories, taskID int64) error {
	for _, deleteRows := range []func(int64) error{
		repos.Transcription.DeleteByTaskID,
		repos.TranscriptionChunk.DeleteByTaskID,
		repos.Summary.DeleteByTaskID,
		repos.VideoChunk.DeleteByTaskID,
		repos.RAGIndex.DeleteByTaskID,
		repos.Chat.DeleteByTaskID,
		repos.TaskJob.DeleteByTaskID,
	} {
		if err := deleteRows(taskID); err != nil {
			return err
		}
	}
	return nil
}
