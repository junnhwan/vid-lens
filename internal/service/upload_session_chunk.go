package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"vid-lens/internal/model"
)

type UploadChunkResult struct {
	ChunkIndex    int    `json:"chunk_index"`
	ActualSize    int64  `json:"actual_size"`
	ContentSHA256 string `json:"content_sha256"`
	ObjectName    string `json:"-"`
}

func (s *UploadSessionService) AcceptChunk(ctx context.Context, userID int64, sessionID string, chunkIndex int, reader io.Reader) (*UploadChunkResult, error) {
	if reader == nil {
		return nil, newUploadSessionError(UploadSessionErrorInvalid, "分片内容不能为空", nil)
	}
	session, err := s.findOwned(userID, sessionID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureChunkUploadAllowed(session, userID); err != nil {
		return nil, err
	}
	expectedSize, err := expectedUploadChunkSize(session, chunkIndex)
	if err != nil {
		return nil, err
	}

	tmp, actualSize, contentSHA256, err := spoolUploadChunk(reader, expectedSize)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	existing, err := s.repos.UploadSession.FindChunk(session.ID, chunkIndex)
	if err != nil {
		return nil, fmt.Errorf("find accepted upload chunk: %w", err)
	}
	if existing != nil {
		return compareAcceptedChunk(existing.ActualSize, existing.ContentSHA256, existing.ObjectName, chunkIndex, actualSize, contentSHA256)
	}
	if s.store == nil {
		return nil, fmt.Errorf("upload object store is not configured")
	}

	objectName := fmt.Sprintf("upload-sessions/%s/chunks/%d/%s", session.ID, chunkIndex, contentSHA256)
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewind upload chunk: %w", err)
	}
	if err := s.store.PutObject(ctx, objectName, tmp, actualSize, "application/octet-stream"); err != nil {
		return nil, fmt.Errorf("store upload chunk: %w", err)
	}

	chunk := &model.UploadSessionChunk{
		SessionID:     session.ID,
		ChunkIndex:    chunkIndex,
		ActualSize:    actualSize,
		ContentSHA256: contentSHA256,
		ObjectName:    objectName,
	}
	if err := s.repos.UploadSession.CreateChunk(chunk); err != nil {
		// Resolve a concurrent insert before deleting. If both requests carried
		// the same bytes, they target the same accepted object and are idempotent.
		winner, findErr := s.repos.UploadSession.FindChunk(session.ID, chunkIndex)
		if findErr == nil && winner != nil {
			if winner.ActualSize == actualSize && winner.ContentSHA256 == contentSHA256 {
				return uploadChunkResult(winner), nil
			}
			_ = s.store.DeleteObject(ctx, objectName)
			return nil, newUploadSessionError(UploadSessionErrorConflict, "该分片序号已上传不同内容", err)
		}
		_ = s.store.DeleteObject(ctx, objectName)
		return nil, fmt.Errorf("record upload chunk: %w", err)
	}
	return uploadChunkResult(chunk), nil
}

func (s *UploadSessionService) ensureChunkUploadAllowed(session *model.UploadSession, userID int64) error {
	now := s.cfg.Now().UTC()
	if session.Status == model.UploadSessionStatusExpired {
		return uploadSessionExpiredError()
	}
	if !session.ExpiresAt.After(now) && (session.Status == model.UploadSessionStatusActive || session.Status == model.UploadSessionStatusCompleting) {
		expired, err := s.repos.UploadSession.MarkExpired(session.ID, userID, now)
		if err != nil {
			return fmt.Errorf("expire upload session: %w", err)
		}
		if expired {
			return uploadSessionExpiredError()
		}
		// A failed expiry CAS for a completing session means its lease is live.
		return newUploadSessionError(UploadSessionErrorInProgress, "上传会话正在完成，请稍后重试", nil)
	}
	switch session.Status {
	case model.UploadSessionStatusActive:
		return nil
	case model.UploadSessionStatusCompleting:
		return newUploadSessionError(UploadSessionErrorInProgress, "上传会话正在完成，请稍后重试", nil)
	case model.UploadSessionStatusCompleted:
		return newUploadSessionError(UploadSessionErrorConflict, "上传会话已完成", nil)
	case model.UploadSessionStatusFailed:
		return newUploadSessionError(UploadSessionErrorFailed, "上传会话已失败，请重新开始上传", nil)
	default:
		return newUploadSessionError(UploadSessionErrorConflict, "上传会话状态不允许继续上传", nil)
	}
}

func expectedUploadChunkSize(session *model.UploadSession, chunkIndex int) (int64, error) {
	if chunkIndex < 0 || chunkIndex >= session.TotalChunks {
		return 0, newUploadSessionError(UploadSessionErrorInvalid, "分片序号越界", nil)
	}
	if chunkIndex < session.TotalChunks-1 {
		return session.ChunkSize, nil
	}
	return session.FileSize - int64(session.TotalChunks-1)*session.ChunkSize, nil
}

func spoolUploadChunk(reader io.Reader, expectedSize int64) (*os.File, int64, string, error) {
	tmp, err := os.CreateTemp("", "vidlens_upload_chunk_*")
	if err != nil {
		return nil, 0, "", fmt.Errorf("create upload chunk temp file: %w", err)
	}
	hasher := sha256.New()
	actualSize, copyErr := io.Copy(io.MultiWriter(tmp, hasher), io.LimitReader(reader, expectedSize+1))
	if copyErr != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, 0, "", fmt.Errorf("read upload chunk: %w", copyErr)
	}
	if actualSize != expectedSize {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, 0, "", newUploadSessionError(
			UploadSessionErrorInvalid,
			fmt.Sprintf("分片大小错误: 实际 %d 字节，期望 %d 字节", actualSize, expectedSize),
			nil,
		)
	}
	return tmp, actualSize, hex.EncodeToString(hasher.Sum(nil)), nil
}

func compareAcceptedChunk(storedSize int64, storedHash, objectName string, chunkIndex int, actualSize int64, actualHash string) (*UploadChunkResult, error) {
	if storedSize != actualSize || storedHash != actualHash {
		return nil, newUploadSessionError(UploadSessionErrorConflict, "该分片序号已上传不同内容", nil)
	}
	return &UploadChunkResult{
		ChunkIndex:    chunkIndex,
		ActualSize:    storedSize,
		ContentSHA256: storedHash,
		ObjectName:    objectName,
	}, nil
}

func uploadChunkResult(chunk *model.UploadSessionChunk) *UploadChunkResult {
	return &UploadChunkResult{
		ChunkIndex:    chunk.ChunkIndex,
		ActualSize:    chunk.ActualSize,
		ContentSHA256: chunk.ContentSHA256,
		ObjectName:    chunk.ObjectName,
	}
}
