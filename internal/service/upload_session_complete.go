package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"vid-lens/internal/model"
	"vid-lens/internal/repository"

	"github.com/google/uuid"
)

var (
	errUploadCompletionCASLost = errors.New("upload completion ownership changed")
	errUploadAssetSizeMismatch = errors.New("upload asset size does not match manifest")
)

type uploadAssemblyResult struct {
	size int64
	md5  string
	err  error
}

// Complete verifies an immutable upload manifest, creates one user task, and
// stores that task identity on the session. The completion token is the only
// authority allowed to commit the asset/task/session transaction.
func (s *UploadSessionService) Complete(ctx context.Context, userID int64, sessionID string) (*UploadResult, error) {
	session, err := s.findOwned(userID, sessionID)
	if err != nil {
		return nil, err
	}
	if session.Status == model.UploadSessionStatusCompleted {
		return s.completedUploadResult(session)
	}
	if session.Status == model.UploadSessionStatusFailed {
		return nil, newUploadSessionError(UploadSessionErrorFailed, "上传会话已失败，请重新开始上传", nil)
	}

	now := s.cfg.Now().UTC()
	if session.Status == model.UploadSessionStatusExpired || !session.ExpiresAt.After(now) {
		if session.Status != model.UploadSessionStatusExpired {
			expired, expireErr := s.repos.UploadSession.MarkExpired(session.ID, userID, now)
			if expireErr != nil {
				return nil, fmt.Errorf("expire upload session before completion: %w", expireErr)
			}
			if !expired {
				return s.resultAfterCompletionClaimConflict(userID, session.ID, now)
			}
		}
		return nil, uploadSessionExpiredError()
	}

	token := uuid.NewString()
	claimed, err := s.repos.UploadSession.ClaimCompletion(repository.UploadSessionClaimRequest{
		SessionID:  session.ID,
		UserID:     userID,
		Token:      token,
		Now:        now,
		LeaseUntil: now.Add(s.cfg.CompletionLease),
	})
	if err != nil {
		return nil, fmt.Errorf("claim upload completion: %w", err)
	}
	if !claimed {
		return s.resultAfterCompletionClaimConflict(userID, session.ID, now)
	}

	asset, err := s.repos.Asset.FindByMD5(session.ExpectedMD5)
	if err != nil {
		return nil, s.releaseCompletionWithError(session, token, fmt.Errorf("find reusable upload asset: %w", err))
	}
	if asset != nil {
		if asset.FileSize != session.FileSize {
			return nil, s.failCompletionIntegrity(session, token, "已存在资产大小与上传清单不一致", errUploadAssetSizeMismatch)
		}
		return s.finalizeUploadCompletion(ctx, session, token, session.ExpectedMD5, asset.ObjectName, asset)
	}

	if s.store == nil {
		return nil, s.releaseCompletionWithError(session, token, errors.New("upload object store is not configured"))
	}
	chunks, err := s.repos.UploadSession.ListChunks(session.ID)
	if err != nil {
		return nil, s.releaseCompletionWithError(session, token, fmt.Errorf("list upload chunks for completion: %w", err))
	}
	if err := validateUploadCompletionChunks(session, chunks); err != nil {
		conflict := newUploadSessionError(UploadSessionErrorConflict, "上传分片不完整，请补齐后重试", err)
		return nil, s.releaseCompletionWithError(session, token, conflict)
	}

	finalObjectName := uploadFinalObjectName(session)
	assembled := assembleUploadChunks(ctx, s.store, chunks, finalObjectName, session.FileSize, contentTypeForFilename(session.Filename))
	if assembled.err != nil {
		_ = s.store.DeleteObject(ctx, finalObjectName)
		return nil, s.releaseCompletionWithError(session, token, fmt.Errorf("assemble upload session: %w", assembled.err))
	}
	if assembled.size != session.FileSize || assembled.md5 != session.ExpectedMD5 {
		_ = s.store.DeleteObject(ctx, finalObjectName)
		cause := fmt.Errorf(
			"assembled upload integrity mismatch: size=%d expected_size=%d md5=%s expected_md5=%s",
			assembled.size,
			session.FileSize,
			assembled.md5,
			session.ExpectedMD5,
		)
		return nil, s.failCompletionIntegrity(session, token, "服务端校验文件内容失败，请重新开始上传", cause)
	}

	result, err := s.finalizeUploadCompletion(ctx, session, token, assembled.md5, finalObjectName, nil)
	if err != nil {
		return nil, err
	}
	for _, chunk := range chunks {
		_ = s.store.DeleteObject(ctx, chunk.ObjectName)
	}
	return result, nil
}

func (s *UploadSessionService) resultAfterCompletionClaimConflict(userID int64, sessionID string, now time.Time) (*UploadResult, error) {
	current, err := s.findOwned(userID, sessionID)
	if err != nil {
		return nil, err
	}
	switch current.Status {
	case model.UploadSessionStatusCompleted:
		return s.completedUploadResult(current)
	case model.UploadSessionStatusExpired:
		return nil, uploadSessionExpiredError()
	case model.UploadSessionStatusFailed:
		return nil, newUploadSessionError(UploadSessionErrorFailed, "上传会话已失败，请重新开始上传", nil)
	case model.UploadSessionStatusCompleting:
		if !current.ExpiresAt.After(now) && (current.CompletionLeaseExpiresAt == nil || !current.CompletionLeaseExpiresAt.After(now)) {
			expired, expireErr := s.repos.UploadSession.MarkExpired(current.ID, userID, now)
			if expireErr != nil {
				return nil, fmt.Errorf("expire stale upload completion: %w", expireErr)
			}
			if expired {
				return nil, uploadSessionExpiredError()
			}
		}
		return nil, newUploadSessionError(UploadSessionErrorInProgress, "上传会话正在完成，请稍后重试", nil)
	case model.UploadSessionStatusActive:
		if !current.ExpiresAt.After(now) {
			return nil, uploadSessionExpiredError()
		}
		return nil, newUploadSessionError(UploadSessionErrorConflict, "上传会话状态已变化，请重试", nil)
	default:
		return nil, newUploadSessionError(UploadSessionErrorConflict, "上传会话状态不允许完成", nil)
	}
}

func validateUploadCompletionChunks(session *model.UploadSession, chunks []model.UploadSessionChunk) error {
	if len(chunks) != session.TotalChunks {
		return fmt.Errorf("accepted chunk count = %d, want %d", len(chunks), session.TotalChunks)
	}
	for index, chunk := range chunks {
		if chunk.ChunkIndex != index {
			return fmt.Errorf("chunk at position %d has index %d", index, chunk.ChunkIndex)
		}
		expectedSize, err := expectedUploadChunkSize(session, index)
		if err != nil {
			return err
		}
		if chunk.ActualSize != expectedSize {
			return fmt.Errorf("chunk %d ledger size = %d, want %d", index, chunk.ActualSize, expectedSize)
		}
		if strings.TrimSpace(chunk.ObjectName) == "" {
			return fmt.Errorf("chunk %d object name is empty", index)
		}
	}
	return nil
}

func uploadFinalObjectName(session *model.UploadSession) string {
	extension := strings.ToLower(path.Ext(session.Filename))
	if len(extension) > 16 || strings.ContainsAny(extension, "/\\") {
		extension = ""
	}
	return fmt.Sprintf("upload-sessions/%s/final%s", session.ID, extension)
}

func assembleUploadChunks(
	ctx context.Context,
	store UploadObjectStore,
	chunks []model.UploadSessionChunk,
	finalObjectName string,
	expectedSize int64,
	contentType string,
) uploadAssemblyResult {
	pipeReader, pipeWriter := io.Pipe()
	copyResult := make(chan uploadAssemblyResult, 1)
	go func() {
		hasher := md5.New()
		result := uploadAssemblyResult{}
		writer := io.MultiWriter(pipeWriter, hasher)
		for _, chunk := range chunks {
			if err := ctx.Err(); err != nil {
				result.err = err
				break
			}
			reader, err := store.OpenObject(ctx, chunk.ObjectName)
			if err != nil {
				result.err = fmt.Errorf("open chunk %d: %w", chunk.ChunkIndex, err)
				break
			}
			n, copyErr := io.Copy(writer, reader)
			closeErr := reader.Close()
			result.size += n
			if copyErr != nil {
				result.err = fmt.Errorf("copy chunk %d: %w", chunk.ChunkIndex, copyErr)
				break
			}
			if closeErr != nil {
				result.err = fmt.Errorf("close chunk %d: %w", chunk.ChunkIndex, closeErr)
				break
			}
		}
		result.md5 = hex.EncodeToString(hasher.Sum(nil))
		_ = pipeWriter.CloseWithError(result.err)
		copyResult <- result
	}()

	putErr := store.PutObject(ctx, finalObjectName, pipeReader, expectedSize, contentType)
	_ = pipeReader.CloseWithError(putErr)
	result := <-copyResult
	if putErr != nil {
		result.err = errors.Join(result.err, fmt.Errorf("store final upload object: %w", putErr))
	}
	return result
}

func (s *UploadSessionService) finalizeUploadCompletion(
	ctx context.Context,
	session *model.UploadSession,
	token string,
	verifiedMD5 string,
	objectName string,
	reusableAsset *model.VideoAsset,
) (*UploadResult, error) {
	var activeAsset *model.VideoAsset
	var task *model.VideoTask
	completedAt := s.cfg.Now().UTC()
	err := s.repos.TransactionContext(ctx, func(txRepos *repository.Repositories) error {
		asset, err := resolveUploadCompletionAsset(txRepos, session, objectName, reusableAsset)
		if err != nil {
			return err
		}
		activeAsset = asset
		task = &model.VideoTask{
			UserID:     session.UserID,
			AssetID:    &asset.ID,
			FileMD5:    asset.FileMD5,
			Filename:   session.Filename,
			FileURL:    asset.ObjectName,
			FileSize:   asset.FileSize,
			Status:     model.TaskStatusPending,
			Stage:      model.TaskStageUploaded,
			TraceID:    uuid.NewString(),
			SourceType: model.TaskSourceTypeChunked,
		}
		if err := txRepos.Task.Create(task); err != nil {
			return fmt.Errorf("create upload task: %w", err)
		}
		updated, err := txRepos.UploadSession.MarkCompleted(repository.UploadSessionCompletion{
			SessionID:       session.ID,
			UserID:          session.UserID,
			Token:           token,
			VerifiedMD5:     verifiedMD5,
			FinalObjectName: asset.ObjectName,
			AssetID:         asset.ID,
			TaskID:          task.ID,
			CompletedAt:     completedAt,
		})
		if err != nil {
			return fmt.Errorf("mark upload session completed: %w", err)
		}
		if !updated {
			return errUploadCompletionCASLost
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errUploadAssetSizeMismatch) {
			return nil, s.failCompletionIntegrity(session, token, "已存在资产大小与上传清单不一致", err)
		}
		return nil, s.releaseCompletionWithError(session, token, fmt.Errorf("finalize upload completion: %w", err))
	}
	return uploadResultFromTask(task, activeAsset), nil
}

func resolveUploadCompletionAsset(
	txRepos *repository.Repositories,
	session *model.UploadSession,
	objectName string,
	reusableAsset *model.VideoAsset,
) (*model.VideoAsset, error) {
	asset := reusableAsset
	if asset == nil {
		asset = &model.VideoAsset{
			FileMD5:     session.ExpectedMD5,
			ObjectName:  objectName,
			FileSize:    session.FileSize,
			ContentType: contentTypeForFilename(session.Filename),
		}
		if err := txRepos.Asset.CreateOrRestore(asset); err != nil {
			existing, findErr := txRepos.Asset.FindByMD5(session.ExpectedMD5)
			if findErr != nil {
				return nil, errors.Join(err, fmt.Errorf("find concurrently created upload asset: %w", findErr))
			}
			if existing == nil {
				return nil, fmt.Errorf("create upload asset: %w", err)
			}
			asset = existing
		}
	}
	locked, err := txRepos.Asset.FindActiveByIDForUpdate(asset.ID)
	if err != nil {
		return nil, fmt.Errorf("lock upload asset: %w", err)
	}
	if locked.FileMD5 != session.ExpectedMD5 || locked.FileSize != session.FileSize {
		return nil, errUploadAssetSizeMismatch
	}
	return locked, nil
}

func (s *UploadSessionService) completedUploadResult(session *model.UploadSession) (*UploadResult, error) {
	if session.TaskID == nil {
		return nil, errors.New("completed upload session has no task identity")
	}
	task, err := s.repos.Task.FindByID(*session.TaskID)
	if err != nil {
		return nil, fmt.Errorf("find completed upload task: %w", err)
	}
	if task.UserID != session.UserID {
		return nil, errors.New("completed upload task owner does not match session")
	}
	asset := &model.VideoAsset{
		FileMD5:    task.FileMD5,
		ObjectName: task.FileURL,
		FileSize:   task.FileSize,
	}
	return uploadResultFromTask(task, asset), nil
}

func uploadResultFromTask(task *model.VideoTask, asset *model.VideoAsset) *UploadResult {
	return &UploadResult{
		TaskID:   task.ID,
		FileMD5:  asset.FileMD5,
		Filename: task.Filename,
		FileURL:  asset.ObjectName,
		FileSize: asset.FileSize,
		Status:   task.Status,
		Stage:    task.Stage,
		TraceID:  task.TraceID,
	}
}

func (s *UploadSessionService) releaseCompletionWithError(session *model.UploadSession, token string, cause error) error {
	updated, err := s.repos.UploadSession.ReleaseCompletion(session.ID, session.UserID, token, boundedUploadSessionError(cause))
	if err != nil {
		return errors.Join(cause, fmt.Errorf("release upload completion: %w", err))
	}
	if !updated {
		return errors.Join(cause, errUploadCompletionCASLost)
	}
	return cause
}

func (s *UploadSessionService) failCompletionIntegrity(session *model.UploadSession, token, message string, cause error) error {
	updated, err := s.repos.UploadSession.MarkFailed(session.ID, session.UserID, token, boundedUploadSessionError(cause))
	domainErr := newUploadSessionError(UploadSessionErrorFailed, message, cause)
	if err != nil {
		return errors.Join(domainErr, fmt.Errorf("mark upload completion failed: %w", err))
	}
	if !updated {
		return errors.Join(domainErr, errUploadCompletionCASLost)
	}
	return domainErr
}

func boundedUploadSessionError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) > 1000 {
		return message[:1000]
	}
	return message
}
