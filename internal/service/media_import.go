package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
	"vid-lens/internal/mq"
	"vid-lens/internal/processing"
	"vid-lens/internal/repository"
)

// ImportOptions is HTTP request identity plus the frozen product options.
// An omitted auto_summary retains the existing manual upload behavior.
type ImportOptions struct {
	processing.Options
	IdempotencyKey string
}

type preparedImport struct {
	options           processing.Options
	key, action, hash string
	intent            processing.Intent
}

func (s *MediaService) lookupImport(ctx context.Context, owner int64, action string, input any, options ImportOptions, local bool) (*preparedImport, *UploadResult, error) {
	normalized, err := processing.Normalize(options.Options, local)
	if err != nil {
		return nil, nil, artifact.Err("invalid_processing_options", 400)
	}
	if err := artifact.ValidateKey(options.IdempotencyKey); err != nil {
		return nil, nil, err
	}
	hash := processing.Fingerprint(struct {
		Input   any
		Options processing.Options
	}{input, normalized})
	prior, err := s.repo.ImportRequest.Lookup(ctx, owner, action, options.IdempotencyKey, hash)
	if err != nil {
		return nil, nil, err
	}
	if prior != nil {
		result := uploadResultForTask(prior)
		result.GenerationID, err = s.repo.ImportRequest.ReadAcceptedGeneration(ctx, owner, action, options.IdempotencyKey)
		return nil, result, err
	}
	return &preparedImport{options: normalized, key: options.IdempotencyKey, action: action, hash: hash}, nil, nil
}

func (s *MediaService) freezeImport(owner int64, request *preparedImport) error {
	if s.profiles == nil {
		return artifact.Err("profile_required", 422)
	}
	var resolved *ResolvedConversationProfile
	var err error
	if provider, ok := s.profiles.(interface {
		GetConversationProfile(int64, int64) (*ResolvedConversationProfile, error)
	}); ok {
		resolved, err = provider.GetConversationProfile(owner, request.options.ProfileID)
	} else if request.options.ProfileID == 0 {
		if provider, ok := s.profiles.(interface {
			GetDefaultConversationProfile(int64) (*ResolvedConversationProfile, error)
		}); ok {
			resolved, err = provider.GetDefaultConversationProfile(owner)
		} else {
			var profile *ai.Profile
			profile, err = s.profiles.GetDefaultAIProfile(owner)
			if profile != nil {
				budget, budgetErr := config.DefaultAgentBudgetConfig().Resolve(nil)
				if budgetErr != nil {
					return budgetErr
				}
				resolved = &ResolvedConversationProfile{Profile: profile, ProfileID: profile.ID, EffectiveAgentBudget: budget}
			}
		}
	} else {
		return artifact.Err("profile_not_found", 422)
	}
	if err != nil || resolved == nil || resolved.Profile == nil {
		return artifact.Err("profile_required", 422)
	}
	if err := ai.RequireAction(*resolved.Profile, "summary"); err != nil {
		return artifact.Err("summary_profile_required", 422)
	}
	preference := ""
	if s.repo.AIProfile != nil {
		preference, err = s.repo.AIProfile.PromptPreference(owner, "summary")
		if err != nil {
			return fmt.Errorf("读取摘要偏好失败: %w", err)
		}
	}
	request.intent = processing.Intent{ID: uuid.NewString(), Version: 1, Options: request.options, GenerationID: uuid.NewString(), ProfileID: resolved.Profile.ID, ProfileFingerprint: processing.FingerprintProfile(*resolved.Profile), SummaryPreference: preference, RecipeVersion: processing.Recipe, PolicyJSON: artifact.JSON(struct {
		Recipe  string             `json:"recipe"`
		Options processing.Options `json:"options"`
	}{processing.Recipe, request.options}), BudgetJSON: artifact.JSON(resolved.EffectiveAgentBudget)}
	return nil
}

func applyImportIntent(task *model.VideoTask, request *preparedImport) error {
	raw, err := json.Marshal(request.intent)
	if err != nil {
		return err
	}
	task.ProcessingIntentJSON = string(raw)
	task.VisualDisabled = !request.options.SummaryVisualEnabled
	task.VisualMode = model.VisualModeOff
	if request.options.SummaryVisualEnabled {
		task.VisualMode = model.VisualModeBoth
	}
	return nil
}

// acceptImport commits the HTTP identity, frozen intent and recoverable dispatch
// together. Only the unique acceptance winner publishes after the outer commit.
func (s *MediaService) acceptImport(ctx context.Context, owner int64, request *preparedImport, jobType, stage string, create func(*repository.Repositories) (*model.VideoTask, error), enqueue func(context.Context, model.VideoTask) error) (*UploadResult, error) {
	if s.mq == nil || enqueue == nil {
		return nil, ErrTaskDispatchUnavailable
	}
	var prepared repository.InitialTaskDispatch
	task, created, err := s.repo.AcceptImport(ctx, owner, request.action, request.key, request.hash, func(tx *repository.Repositories) (*model.VideoTask, error) {
		task, err := create(tx)
		if err != nil {
			return nil, err
		}
		if err = applyImportIntent(task, request); err != nil {
			return nil, err
		}
		now := time.Now()
		prepared, err = tx.PrepareInitialTaskDispatch(repository.InitialTaskDispatchRequest{Task: task, CreateTask: true, JobType: jobType, Stage: stage, Now: now, LeaseUntil: now.Add(initialDispatchLease), Token: uuid.NewString()})
		if err != nil {
			return nil, err
		}
		return &prepared.Task, nil
	})
	if err != nil {
		return nil, err
	}
	if !created {
		result := uploadResultForTask(task)
		result.GenerationID, err = s.repo.ImportRequest.ReadAcceptedGeneration(ctx, owner, request.action, request.key)
		return result, err
	}
	enqueueCtx := mq.ContextWithClaimToken(mq.ContextWithRetryBudgetID(mq.ContextWithTraceID(ctx, task.TraceID), prepared.RetryBudgetID), prepared.Token)
	if err := enqueue(enqueueCtx, *task); err != nil {
		_, restoreErr := s.repo.RestoreRetryDispatch(repository.TaskDispatchRestoreRequest{TaskID: task.ID, JobType: jobType, Stage: stage, Token: prepared.Token, ErrorMessage: initialDispatchErrorMessage(err), NextRetryAt: time.Now().Add(initialDispatchFailureBackoff)})
		return nil, publicInitialDispatchError(ctx, *task, jobType, stage, fmt.Errorf("publish accepted import: %w; restore: %v", err, restoreErr))
	}
	result := uploadResultForTask(task)
	result.GenerationID = request.intent.GenerationID
	return result, nil
}

func uploadResultForTask(task *model.VideoTask) *UploadResult {
	result := &UploadResult{TaskID: task.ID, FileMD5: task.FileMD5, Filename: task.Filename, FileURL: task.FileURL, FileSize: task.FileSize, Status: task.Status, Stage: task.Stage, TraceID: task.TraceID}
	if task.ProcessingIntentJSON != "" {
		if intent, err := processing.Decode(task.ProcessingIntentJSON); err == nil {
			result.GenerationID = intent.GenerationID
		}
	}
	return result
}

func (s *MediaService) createTaskFromAssetWithImport(ctx context.Context, owner int64, filename string, asset *model.VideoAsset, request *preparedImport) (*UploadResult, error) {
	if request == nil {
		return s.createTaskFromAsset(owner, filename, asset, model.TaskStatusPending)
	}
	producer, ok := s.mq.(interface {
		EnqueueTextSource(context.Context, int64, string) error
	})
	if !ok {
		return nil, ErrTaskDispatchUnavailable
	}
	return s.acceptImport(ctx, owner, request, model.TaskJobTypeTextSource, model.TaskStageTextSource, func(tx *repository.Repositories) (*model.VideoTask, error) {
		locked, err := tx.Asset.FindActiveByIDForUpdate(asset.ID)
		if err != nil {
			return nil, err
		}
		return &model.VideoTask{UserID: owner, AssetID: &locked.ID, FileMD5: locked.FileMD5, Filename: strings.TrimSpace(filename), FileURL: locked.ObjectName, FileSize: locked.FileSize, Status: model.TaskStatusPending, Stage: model.TaskStageUploaded, TraceID: uuid.NewString(), SourceType: model.TaskSourceTypeChunked, MaxRetries: 3}, nil
	}, func(ctx context.Context, task model.VideoTask) error {
		return producer.EnqueueTextSource(ctx, task.ID, task.FileMD5)
	})
}
