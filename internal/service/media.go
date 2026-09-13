package service

import (
	"context"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/redis/go-redis/v9"
	"vid-lens/internal/config"
	"vid-lens/internal/mq"
	"vid-lens/internal/repository"
	"vid-lens/internal/storage"
)

// MediaService 的依赖、构造函数和跨上传流程共享类型。
//
// MediaService 是 handler 使用的统一 façade；具体实现按能力拆分在同目录的
// media_file_upload.go、media_url_upload.go 和 media_tasks.go 中。拆分文件共享同一个
// MediaService，不要为了文件边界
// 重复创建 service 或绕过这里的仓储、对象存储和消息队列依赖。
type mediaProducer interface {
	EnqueueAnalyze(ctx context.Context, taskID int64, md5 string) error
	EnqueueTranscribe(ctx context.Context, taskID int64, md5 string) error
	EnqueueDownload(ctx context.Context, taskID int64, key string) error
}

type objectDeleter interface {
	DeleteObject(ctx context.Context, objectName string) error
}

// mediaObjectStore is the object-storage surface MediaService needs. It stays
// an interface so tests can exercise playback and cleanup logic without a
// running MinIO; *storage.MinIOStorage is the production implementation.
type mediaObjectStore interface {
	objectDeleter
	UploadFile(ctx context.Context, objectName string, reader io.Reader, size int64, contentType string) error
	UploadFromPath(ctx context.Context, localPath, objectName, contentType string) (int64, error)
	BucketName() string
	ComposeObject(ctx context.Context, dst string, sources []minio.CopySrcOptions) (int64, error)
	OpenObject(ctx context.Context, objectName string) (storage.Object, error)
	ObjectContentType(ctx context.Context, objectName string) string
	GetPresignedURL(ctx context.Context, objectName string) (string, error)
}

type TaskVectorCleaner interface {
	DeleteTaskChunks(ctx context.Context, userID, taskID int64, embeddingModel string) error
}

type MediaService struct {
	repo              *repository.Repositories
	storage           mediaObjectStore
	taskCleanup       *TaskCleanupService
	remoteURLResolver remoteURLResolver
	mq                mediaProducer
	rdb               redis.Cmdable
	cfg               config.UploadConfig
	tools             config.ToolsConfig
	// playbackSecret signs the task-scoped credentials embedded in playback
	// URLs. Browser media elements cannot send an Authorization header, so the
	// credential has to travel in the URL itself.
	playbackSecret string
}

func NewMediaService(
	repo *repository.Repositories,
	storage mediaObjectStore,
	mqProducer *mq.Producer,
	rdb redis.Cmdable,
	cfg config.UploadConfig,
	tools config.ToolsConfig,
	jwtCfg config.JWTConfig,
) *MediaService {
	return &MediaService{
		repo:           repo,
		storage:        storage,
		mq:             mqProducer,
		rdb:            rdb,
		cfg:            cfg,
		tools:          tools,
		playbackSecret: jwtCfg.Secret,
	}
}

func (s *MediaService) SetTaskCleanupService(cleanup *TaskCleanupService) {
	s.taskCleanup = cleanup
}

type UploadResult struct {
	TaskID   int64  `json:"task_id"`
	FileMD5  string `json:"file_md5"`
	Filename string `json:"filename"`
	FileURL  string `json:"file_url"`
	FileSize int64  `json:"file_size"`
	Status   int8   `json:"status"`
	Stage    string `json:"stage"`
	TraceID  string `json:"trace_id"`
}
