package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"vid-lens/internal/model"
	"vid-lens/internal/repository"

	"github.com/google/uuid"
)

const (
	defaultUploadSessionTTL      = 24 * time.Hour
	defaultUploadCompletionLease = 2 * time.Minute
)

// UploadObjectStore is the byte-storage boundary needed by durable upload
// sessions. PostgreSQL lifecycle logic remains testable without a real MinIO.
type UploadObjectStore interface {
	PutObject(ctx context.Context, objectName string, reader io.Reader, size int64, contentType string) error
	OpenObject(ctx context.Context, objectName string) (io.ReadCloser, error)
	DeleteObject(ctx context.Context, objectName string) error
}

type UploadSessionConfig struct {
	MaxFileSize     int64
	MaxChunkSize    int64
	SessionTTL      time.Duration
	CompletionLease time.Duration
	Now             func() time.Time
}

type CreateUploadSessionRequest struct {
	Filename    string `json:"filename"`
	FileSize    int64  `json:"file_size"`
	ChunkSize   int64  `json:"chunk_size"`
	TotalChunks int    `json:"total_chunks"`
	ExpectedMD5 string `json:"expected_md5"`
}

type UploadSessionView struct {
	SessionID      string    `json:"session_id"`
	Filename       string    `json:"filename"`
	FileSize       int64     `json:"file_size"`
	ChunkSize      int64     `json:"chunk_size"`
	TotalChunks    int       `json:"total_chunks"`
	ExpectedMD5    string    `json:"expected_md5"`
	Status         string    `json:"status"`
	Uploaded       []int     `json:"uploaded"`
	AssetAvailable bool      `json:"asset_available"`
	ExpiresAt      time.Time `json:"expires_at"`
	TaskID         *int64    `json:"task_id,omitempty"`
	LastError      string    `json:"last_error,omitempty"`
}

// UploadSessionService owns upload-session domain rules. It intentionally has
// no Redis dependency: manifests and progress are always reconstructed from
// PostgreSQL.
type UploadSessionService struct {
	repos *repository.Repositories
	store UploadObjectStore
	cfg   UploadSessionConfig
}

func NewUploadSessionService(repos *repository.Repositories, store UploadObjectStore, cfg UploadSessionConfig) *UploadSessionService {
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = defaultUploadSessionTTL
	}
	if cfg.CompletionLease <= 0 {
		cfg.CompletionLease = defaultUploadCompletionLease
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &UploadSessionService{repos: repos, store: store, cfg: cfg}
}

func (s *UploadSessionService) Create(ctx context.Context, userID int64, req CreateUploadSessionRequest) (*UploadSessionView, error) {
	_ = ctx
	filename, err := s.validateManifest(userID, req)
	if err != nil {
		return nil, err
	}
	req.Filename = filename
	fingerprint := uploadManifestFingerprint(req)
	activeKey := uploadActiveKey(userID, fingerprint)
	now := s.cfg.Now().UTC()

	existing, err := s.repos.UploadSession.FindByActiveKey(userID, activeKey)
	if err != nil {
		return nil, fmt.Errorf("find active upload session: %w", err)
	}
	if existing != nil {
		if existing.ExpiresAt.After(now) {
			return s.view(existing)
		}
		expired, err := s.repos.UploadSession.MarkExpired(existing.ID, userID, now)
		if err != nil {
			return nil, fmt.Errorf("expire upload session: %w", err)
		}
		if !expired {
			// A live completion lease owns the expired manifest until it either
			// finishes or its lease becomes stale. Do not create a competing session.
			current, findErr := s.repos.UploadSession.FindByActiveKey(userID, activeKey)
			if findErr != nil {
				return nil, fmt.Errorf("reload active upload session: %w", findErr)
			}
			if current != nil {
				return s.view(current)
			}
		}
	}

	session := &model.UploadSession{
		ID:                  uuid.NewString(),
		UserID:              userID,
		Filename:            req.Filename,
		FileSize:            req.FileSize,
		ChunkSize:           req.ChunkSize,
		TotalChunks:         req.TotalChunks,
		ExpectedMD5:         strings.ToLower(req.ExpectedMD5),
		ManifestFingerprint: fingerprint,
		ActiveKey:           &activeKey,
		Status:              model.UploadSessionStatusActive,
		ExpiresAt:           now.Add(s.cfg.SessionTTL),
	}
	if err := s.repos.UploadSession.Create(session); err != nil {
		// A concurrent request can win the unique active_key race. Read the
		// winner so create/resume remains idempotent across API instances.
		winner, findErr := s.repos.UploadSession.FindByActiveKey(userID, activeKey)
		if findErr == nil && winner != nil {
			return s.view(winner)
		}
		return nil, fmt.Errorf("create upload session: %w", err)
	}
	return s.view(session)
}

func (s *UploadSessionService) Get(ctx context.Context, userID int64, sessionID string) (*UploadSessionView, error) {
	_ = ctx
	session, err := s.findOwned(userID, sessionID)
	if err != nil {
		return nil, err
	}
	now := s.cfg.Now().UTC()
	if session.Status == model.UploadSessionStatusExpired {
		return nil, uploadSessionExpiredError()
	}
	if (session.Status == model.UploadSessionStatusActive || session.Status == model.UploadSessionStatusCompleting) && !session.ExpiresAt.After(now) {
		expired, expireErr := s.repos.UploadSession.MarkExpired(session.ID, userID, now)
		if expireErr != nil {
			return nil, fmt.Errorf("expire upload session: %w", expireErr)
		}
		if expired {
			return nil, uploadSessionExpiredError()
		}

		// MarkExpired intentionally preserves a live completion lease. Reload
		// after the failed CAS so GET never reports a stale lifecycle snapshot.
		session, err = s.findOwned(userID, sessionID)
		if err != nil {
			return nil, err
		}
		if session.Status == model.UploadSessionStatusExpired {
			return nil, uploadSessionExpiredError()
		}
	}
	return s.view(session)
}

func uploadSessionExpiredError() error {
	return newUploadSessionError(UploadSessionErrorExpired, "上传会话已过期，请重新开始上传", nil)
}

func (s *UploadSessionService) findOwned(userID int64, sessionID string) (*model.UploadSession, error) {
	if userID <= 0 || strings.TrimSpace(sessionID) == "" {
		return nil, newUploadSessionError(UploadSessionErrorNotFound, "上传会话不存在", nil)
	}
	session, err := s.repos.UploadSession.FindByIDForUser(sessionID, userID)
	if err != nil {
		return nil, fmt.Errorf("find upload session: %w", err)
	}
	if session == nil {
		return nil, newUploadSessionError(UploadSessionErrorNotFound, "上传会话不存在", nil)
	}
	return session, nil
}

func (s *UploadSessionService) view(session *model.UploadSession) (*UploadSessionView, error) {
	chunks, err := s.repos.UploadSession.ListChunks(session.ID)
	if err != nil {
		return nil, fmt.Errorf("list upload chunks: %w", err)
	}
	uploaded := make([]int, 0, len(chunks))
	for _, chunk := range chunks {
		uploaded = append(uploaded, chunk.ChunkIndex)
	}
	asset, err := s.repos.Asset.FindByMD5(session.ExpectedMD5)
	if err != nil {
		return nil, fmt.Errorf("find upload asset: %w", err)
	}
	return &UploadSessionView{
		SessionID:      session.ID,
		Filename:       session.Filename,
		FileSize:       session.FileSize,
		ChunkSize:      session.ChunkSize,
		TotalChunks:    session.TotalChunks,
		ExpectedMD5:    session.ExpectedMD5,
		Status:         session.Status,
		Uploaded:       uploaded,
		AssetAvailable: asset != nil && asset.FileSize == session.FileSize,
		ExpiresAt:      session.ExpiresAt,
		TaskID:         session.TaskID,
		LastError:      session.LastError,
	}, nil
}

func (s *UploadSessionService) validateManifest(userID int64, req CreateUploadSessionRequest) (string, error) {
	filename := path.Base(strings.ReplaceAll(strings.TrimSpace(req.Filename), "\\", "/"))
	if userID <= 0 {
		return "", newUploadSessionError(UploadSessionErrorInvalid, "用户身份无效", nil)
	}
	if filename == "" || filename == "." || filename == "/" {
		return "", newUploadSessionError(UploadSessionErrorInvalid, "文件名不能为空", nil)
	}
	if len([]byte(filename)) > 255 {
		return "", newUploadSessionError(UploadSessionErrorInvalid, "文件名过长", nil)
	}
	if req.FileSize <= 0 {
		return "", newUploadSessionError(UploadSessionErrorInvalid, "文件大小必须大于 0", nil)
	}
	if s.cfg.MaxFileSize > 0 && req.FileSize > s.cfg.MaxFileSize {
		return "", newUploadSessionError(UploadSessionErrorInvalid, fmt.Sprintf("文件大小超过限制: 最大 %d 字节", s.cfg.MaxFileSize), nil)
	}
	if req.ChunkSize <= 0 {
		return "", newUploadSessionError(UploadSessionErrorInvalid, "分片大小必须大于 0", nil)
	}
	if s.cfg.MaxChunkSize > 0 && req.ChunkSize > s.cfg.MaxChunkSize {
		return "", newUploadSessionError(UploadSessionErrorInvalid, fmt.Sprintf("分片大小超过限制: 最大 %d 字节", s.cfg.MaxChunkSize), nil)
	}
	if req.TotalChunks <= 0 || int64(req.TotalChunks) != (req.FileSize+req.ChunkSize-1)/req.ChunkSize {
		return "", newUploadSessionError(UploadSessionErrorInvalid, "分片总数与文件大小不匹配", nil)
	}
	if err := validateFileMD5(strings.ToLower(req.ExpectedMD5)); err != nil {
		return "", newUploadSessionError(UploadSessionErrorInvalid, err.Error(), err)
	}
	return filename, nil
}

func uploadManifestFingerprint(req CreateUploadSessionRequest) string {
	canonical := fmt.Sprintf("%s\x00%d\x00%d\x00%d\x00%s", req.Filename, req.FileSize, req.ChunkSize, req.TotalChunks, strings.ToLower(req.ExpectedMD5))
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

func uploadActiveKey(userID int64, fingerprint string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s", userID, fingerprint)))
	return hex.EncodeToString(sum[:])
}
