package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	neturl "net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/redis/go-redis/v9"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
	"vid-lens/internal/mq"
	"vid-lens/internal/pkg/lock"
	"vid-lens/internal/repository"
	"vid-lens/internal/storage"
)

type mediaProducer interface {
	EnqueueAnalyze(ctx context.Context, taskID int64, md5 string) error
	EnqueueTranscribe(ctx context.Context, taskID int64, md5 string) error
	EnqueueDownload(ctx context.Context, taskID int64, key string) error
}

type objectDeleter interface {
	DeleteObject(ctx context.Context, objectName string) error
}

type TaskVectorCleaner interface {
	DeleteTaskChunks(ctx context.Context, userID, taskID int64, embeddingModel string) error
}

type MediaService struct {
	repo              *repository.Repositories
	storage           *storage.MinIOStorage
	objectDeleter     objectDeleter
	taskVectorCleaner TaskVectorCleaner
	remoteURLResolver remoteURLResolver
	mq                mediaProducer
	rdb               redis.Cmdable
	cfg               config.UploadConfig
	tools             config.ToolsConfig
}

func NewMediaService(
	repo *repository.Repositories,
	storage *storage.MinIOStorage,
	mqProducer *mq.Producer,
	rdb redis.Cmdable,
	cfg config.UploadConfig,
	tools config.ToolsConfig,
) *MediaService {
	return &MediaService{
		repo:          repo,
		storage:       storage,
		objectDeleter: storage,
		mq:            mqProducer,
		rdb:           rdb,
		cfg:           cfg,
		tools:         tools,
	}
}

func (s *MediaService) SetTaskVectorCleaner(cleaner TaskVectorCleaner) {
	s.taskVectorCleaner = cleaner
}

func (s *MediaService) MaxChunkSize() int64 {
	return s.cfg.ChunkSize
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

// UploadFile 普通文件上传
func (s *MediaService) UploadFile(ctx context.Context, userID int64, filename string, fileStream io.Reader, fileSize int64) (*UploadResult, error) {
	if err := s.validateUploadSize(fileSize); err != nil {
		return nil, err
	}

	tmpPath, fileMD5, actualSize, err := copyStreamToTempAndHash(fileStream, s.cfg.MaxFileSize)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}
	defer os.Remove(tmpPath)

	// 内容级去重
	asset, err := s.repo.Asset.FindByMD5(fileMD5)
	if err != nil {
		return nil, err
	}

	ext := filepath.Ext(filename)
	objectName := fmt.Sprintf("videos/%s%s", uuid.New().String(), ext)
	if asset == nil {
		asset, err = s.createAssetFromLocalFile(ctx, fileMD5, tmpPath, objectName, contentTypeForFilename(filename), actualSize)
		if err != nil {
			return nil, err
		}
	}

	return s.createTaskFromAsset(userID, filename, asset, model.TaskStatusPending)
}

func (s *MediaService) createTaskFromAsset(userID int64, filename string, asset *model.VideoAsset, status int8) (*UploadResult, error) {
	task := &model.VideoTask{
		UserID:   userID,
		AssetID:  &asset.ID,
		FileMD5:  asset.FileMD5,
		Filename: filename,
		FileURL:  asset.ObjectName,
		FileSize: asset.FileSize,
		Status:   status,
		Stage:    model.TaskStageUploaded,
		TraceID:  uuid.New().String(),
	}
	if err := s.repo.Task.Create(task); err != nil {
		return nil, err
	}

	return &UploadResult{
		TaskID:   task.ID,
		FileMD5:  asset.FileMD5,
		Filename: filename,
		FileURL:  asset.ObjectName,
		FileSize: asset.FileSize,
		Status:   status,
		Stage:    task.Stage,
		TraceID:  task.TraceID,
	}, nil
}

// UploadByURL 创建 URL 下载任务并立即返回，真正下载由 Kafka consumer 执行。
func (s *MediaService) UploadByURL(ctx context.Context, userID int64, videoURL string) (*UploadResult, error) {
	checkedURL, err := newRemoteVideoURLValidator(s.tools, s.remoteURLResolver).validate(ctx, videoURL)
	if err != nil {
		return nil, err
	}

	key := md5HexString(checkedURL.Sanitized)
	now := time.Now()
	task := &model.VideoTask{
		UserID:     userID,
		FileMD5:    key,
		Filename:   filenameForURLTask(checkedURL.Sanitized),
		Status:     model.TaskStatusRunning,
		Stage:      model.TaskStageDownloading,
		TraceID:    uuid.New().String(),
		SourceType: model.TaskSourceTypeURL,
		SourceURL:  checkedURL.Sanitized,
		MaxRetries: 3,
		StartedAt:  &now,
	}
	if err := s.repo.Task.Create(task); err != nil {
		return nil, err
	}
	if err := s.repo.TaskJob.UpsertQueued(task, model.TaskJobTypeDownload, model.TaskStageDownloading, task.MaxRetries); err != nil {
		return nil, err
	}

	if err := s.mq.EnqueueDownload(mq.ContextWithTraceID(ctx, task.TraceID), task.ID, key); err != nil {
		errMsg := "下载任务投递失败"
		_ = s.repo.Task.UpdateStatusAndStage(task.ID, model.TaskStatusFailed, model.TaskStageDownloading, errMsg)
		_ = s.repo.TaskJob.RecordTerminalFailure(task.ID, model.TaskJobTypeDownload, model.TaskStageDownloading, "enqueue_failed", errMsg, task.RetryCount, task.MaxRetries, model.TaskStatusFailed)
		return nil, fmt.Errorf("%s: %w", errMsg, err)
	}

	return &UploadResult{
		TaskID:   task.ID,
		FileMD5:  task.FileMD5,
		Filename: task.Filename,
		FileURL:  task.FileURL,
		FileSize: task.FileSize,
		Status:   task.Status,
		Stage:    task.Stage,
		TraceID:  task.TraceID,
	}, nil
}

func md5HexString(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func filenameForURLTask(videoURL string) string {
	parsed, err := neturl.Parse(videoURL)
	if err != nil || parsed.Hostname() == "" {
		return "WEB_remote_video.mp4"
	}
	host := strings.ReplaceAll(parsed.Hostname(), ":", "_")
	return "WEB_" + host + ".mp4"
}

func (s *MediaService) validateUploadSize(fileSize int64) error {
	if s.cfg.MaxFileSize > 0 && fileSize > s.cfg.MaxFileSize {
		return fmt.Errorf("文件大小超过限制: 最大 %d 字节", s.cfg.MaxFileSize)
	}
	return nil
}

func (s *MediaService) createAssetFromLocalFile(ctx context.Context, fileMD5, localPath, objectName, contentType string, size int64) (*model.VideoAsset, error) {
	if _, err := s.storage.UploadFromPath(ctx, localPath, objectName, contentType); err != nil {
		return nil, fmt.Errorf("上传到 MinIO 失败: %w", err)
	}

	asset := &model.VideoAsset{
		FileMD5:     fileMD5,
		ObjectName:  objectName,
		FileSize:    size,
		ContentType: contentType,
	}
	if err := s.repo.Asset.Create(asset); err != nil {
		// 并发上传同一内容时，可能另一个请求已经创建了资产。
		// 复用已存在资产，并删除当前请求刚上传但不再需要的对象。
		existing, findErr := s.repo.Asset.FindByMD5(fileMD5)
		if findErr == nil && existing != nil {
			_ = s.storage.DeleteObject(ctx, objectName)
			return existing, nil
		}
		_ = s.storage.DeleteObject(ctx, objectName)
		return nil, err
	}

	return asset, nil
}

func copyStreamToTempAndHash(r io.Reader, maxSize int64) (string, string, int64, error) {
	tmp, err := os.CreateTemp("", "vidlens_upload_*")
	if err != nil {
		return "", "", 0, err
	}
	defer tmp.Close()

	hasher := md5.New()
	reader := r
	if maxSize > 0 {
		reader = io.LimitReader(r, maxSize+1)
	}

	size, err := io.Copy(tmp, io.TeeReader(reader, hasher))
	if err != nil {
		os.Remove(tmp.Name())
		return "", "", 0, err
	}
	if maxSize > 0 && size > maxSize {
		os.Remove(tmp.Name())
		return "", "", 0, fmt.Errorf("文件大小超过限制: 最大 %d 字节", maxSize)
	}
	if size == 0 {
		os.Remove(tmp.Name())
		return "", "", 0, fmt.Errorf("文件内容为空")
	}

	return tmp.Name(), hex.EncodeToString(hasher.Sum(nil)), size, nil
}

func hashLocalFile(path string, maxSize int64) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()

	hasher := md5.New()
	reader := io.Reader(file)
	if maxSize > 0 {
		reader = io.LimitReader(file, maxSize+1)
	}

	size, err := io.Copy(hasher, reader)
	if err != nil {
		return "", 0, err
	}
	if maxSize > 0 && size > maxSize {
		return "", 0, fmt.Errorf("文件大小超过限制: 最大 %d 字节", maxSize)
	}
	if size == 0 {
		return "", 0, fmt.Errorf("文件内容为空")
	}

	return hex.EncodeToString(hasher.Sum(nil)), size, nil
}

func contentTypeForFilename(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".avi":
		return "video/x-msvideo"
	case ".mkv":
		return "video/x-matroska"
	case ".mov":
		return "video/quicktime"
	case ".webm":
		return "video/webm"
	default:
		return "video/mp4"
	}
}

func validateFileMD5(fileMD5 string) error {
	if len(fileMD5) != 32 {
		return fmt.Errorf("file_md5 格式错误")
	}
	if _, err := hex.DecodeString(fileMD5); err != nil {
		return fmt.Errorf("file_md5 格式错误")
	}
	return nil
}

// RequestAnalysis 提交 AI 分析
func (s *MediaService) RequestAnalysis(ctx context.Context, userID, taskID int64) error {
	task, err := s.repo.Task.FindByID(taskID)
	if err != nil {
		return fmt.Errorf("任务不存在")
	}
	if task.UserID != userID {
		return fmt.Errorf("无权操作此任务")
	}
	if task.Status == model.TaskStatusRunning || task.Status == model.TaskStatusQueued {
		return fmt.Errorf("任务正在处理中，请勿重复提交")
	}
	if task.Status == model.TaskStatusCompleted {
		summary, err := s.repo.Summary.FindByTaskID(task.ID)
		if err != nil {
			return err
		}
		if summary != nil {
			return fmt.Errorf("任务已完成，可直接查看结果")
		}
	}

	updated, err := s.repo.Task.UpdateStatusAndStageIf(taskID,
		[]int8{model.TaskStatusPending, model.TaskStatusFailed, model.TaskStatusCompleted},
		model.TaskStatusQueued, model.TaskStageSummarizing, "")
	if err != nil {
		return err
	}
	if !updated {
		return fmt.Errorf("任务状态已变化，请刷新后重试")
	}
	if err := s.repo.TaskJob.UpsertQueued(task, model.TaskJobTypeAnalyze, model.TaskStageSummarizing, task.MaxRetries); err != nil {
		return err
	}
	if err := s.mq.EnqueueAnalyze(mq.ContextWithTraceID(ctx, task.TraceID), taskID, task.FileMD5); err != nil {
		s.repo.Task.UpdateStatusAndStageIf(taskID, []int8{model.TaskStatusQueued}, model.TaskStatusPending, task.Stage, "消息投递失败")
		_ = s.repo.TaskJob.RecordTerminalFailure(taskID, model.TaskJobTypeAnalyze, model.TaskStageSummarizing, "enqueue_failed", "消息投递失败", task.RetryCount, task.MaxRetries, model.TaskStatusFailed)
		return fmt.Errorf("系统繁忙，请稍后重试")
	}
	return nil
}

// RequestTranscribe 提交文字提取
func (s *MediaService) RequestTranscribe(ctx context.Context, userID, taskID int64) error {
	task, err := s.repo.Task.FindByID(taskID)
	if err != nil {
		return fmt.Errorf("任务不存在")
	}
	if task.UserID != userID {
		return fmt.Errorf("无权操作此任务")
	}
	if task.Status == model.TaskStatusRunning || task.Status == model.TaskStatusQueued {
		return fmt.Errorf("任务正在处理中")
	}
	if task.Status == model.TaskStatusCompleted {
		transcription, err := s.repo.Transcription.FindByTaskID(task.ID)
		if err != nil {
			return err
		}
		if transcription != nil {
			return fmt.Errorf("文字提取已完成，可直接查看结果")
		}
	}

	updated, err := s.repo.Task.UpdateStatusAndStageIf(taskID,
		[]int8{model.TaskStatusPending, model.TaskStatusFailed, model.TaskStatusCompleted},
		model.TaskStatusQueued, model.TaskStageTranscribing, "")
	if err != nil {
		return err
	}
	if !updated {
		return fmt.Errorf("任务状态已变化，请刷新后重试")
	}
	if err := s.repo.TaskJob.UpsertQueued(task, model.TaskJobTypeTranscribe, model.TaskStageTranscribing, task.MaxRetries); err != nil {
		return err
	}
	if err := s.mq.EnqueueTranscribe(mq.ContextWithTraceID(ctx, task.TraceID), taskID, task.FileMD5); err != nil {
		s.repo.Task.UpdateStatusAndStageIf(taskID, []int8{model.TaskStatusQueued}, model.TaskStatusPending, task.Stage, "消息投递失败")
		_ = s.repo.TaskJob.RecordTerminalFailure(taskID, model.TaskJobTypeTranscribe, model.TaskStageTranscribing, "enqueue_failed", "消息投递失败", task.RetryCount, task.MaxRetries, model.TaskStatusFailed)
		return fmt.Errorf("系统繁忙，请稍后重试")
	}
	return nil
}

// GetTaskDetail 获取任务详情
func (s *MediaService) GetTaskDetail(ctx context.Context, userID, taskID int64) (*model.VideoTask, error) {
	task, err := s.repo.Task.FindByIDWithDetail(taskID)
	if err != nil {
		return nil, err
	}
	if task.UserID != userID {
		return nil, fmt.Errorf("无权访问此任务")
	}
	return task, nil
}

// ListTasks 分页查询，keyword 非空时按文件名/标题搜索
func (s *MediaService) ListTasks(userID int64, page, pageSize int, keyword string) ([]model.VideoTask, int64, error) {
	return s.repo.Task.ListByUserID(userID, page, pageSize, keyword)
}

// DeleteTask 删除
func (s *MediaService) DeleteTask(ctx context.Context, userID, taskID int64) error {
	task, err := s.repo.Task.FindByID(taskID)
	if err != nil {
		return fmt.Errorf("任务不存在")
	}
	if task.UserID != userID {
		return fmt.Errorf("无权删除此任务")
	}

	embeddingModels, err := s.collectTaskEmbeddingModels(userID, taskID)
	if err != nil {
		return err
	}
	if s.taskVectorCleaner != nil {
		for _, modelName := range embeddingModels {
			if err := s.taskVectorCleaner.DeleteTaskChunks(ctx, userID, taskID, modelName); err != nil {
				return fmt.Errorf("清理向量数据失败: %w", err)
			}
		}
	}

	assetID := task.AssetID
	objectName := task.FileURL
	deleteAssetObject := false
	if err := s.repo.Transaction(func(txRepos *repository.Repositories) error {
		if err := txRepos.Transcription.DeleteByTaskID(taskID); err != nil {
			return err
		}
		if err := txRepos.TranscriptionChunk.DeleteByTaskID(taskID); err != nil {
			return err
		}
		if err := txRepos.Summary.DeleteByTaskID(taskID); err != nil {
			return err
		}
		if err := txRepos.VideoChunk.DeleteByTaskID(taskID); err != nil {
			return err
		}
		if err := txRepos.RAGIndex.DeleteByTaskID(taskID); err != nil {
			return err
		}
		if err := txRepos.Chat.DeleteByTaskID(taskID); err != nil {
			return err
		}
		if err := txRepos.TaskJob.DeleteByTaskID(taskID); err != nil {
			return err
		}
		if err := txRepos.Task.Delete(taskID); err != nil {
			return err
		}
		if assetID != nil {
			activeRefs, err := txRepos.Task.CountActiveByAssetID(*assetID)
			if err != nil {
				return err
			}
			deleteAssetObject = *assetID > 0 && activeRefs == 0
		}
		return nil
	}); err != nil {
		return err
	}

	if deleteAssetObject && objectName != "" {
		if err := s.deleteObject(ctx, objectName); err != nil {
			return fmt.Errorf("删除视频对象失败: %w", err)
		}
		if err := s.repo.Asset.Delete(*assetID); err != nil {
			return err
		}
	}
	return nil
}

func (s *MediaService) collectTaskEmbeddingModels(userID, taskID int64) ([]string, error) {
	seen := make(map[string]bool)
	addModels := func(models []string) {
		for _, modelName := range models {
			modelName = strings.TrimSpace(modelName)
			if modelName != "" {
				seen[modelName] = true
			}
		}
	}

	chunkModels, err := s.repo.VideoChunk.ListEmbeddingModelsByTask(userID, taskID)
	if err != nil {
		return nil, err
	}
	addModels(chunkModels)
	indexModels, err := s.repo.RAGIndex.ListEmbeddingModelsByTask(userID, taskID)
	if err != nil {
		return nil, err
	}
	addModels(indexModels)

	models := make([]string, 0, len(seen))
	for modelName := range seen {
		models = append(models, modelName)
	}
	return models, nil
}

func (s *MediaService) deleteObject(ctx context.Context, objectName string) error {
	if s.objectDeleter != nil {
		return s.objectDeleter.DeleteObject(ctx, objectName)
	}
	if s.storage == nil {
		return fmt.Errorf("对象存储未启用")
	}
	return s.storage.DeleteObject(ctx, objectName)
}

// GetPresignedURL 获取预签名链接
func (s *MediaService) GetPresignedURL(ctx context.Context, taskID int64) (string, error) {
	task, err := s.repo.Task.FindByID(taskID)
	if err != nil {
		return "", err
	}
	return s.storage.GetPresignedURL(ctx, task.FileURL)
}

// ===== 分片上传 =====

func (s *MediaService) InitChunkedUpload(ctx context.Context, fileMD5 string, totalChunks int) error {
	key := fmt.Sprintf("upload:chunks:%s", fileMD5)
	s.rdb.Set(ctx, key+":total", totalChunks, 24*time.Hour)
	s.rdb.Set(ctx, key+":status", "INIT", 24*time.Hour)
	return nil
}

func (s *MediaService) CheckUploadProgress(ctx context.Context, fileMD5 string) (map[string]any, error) {
	if err := validateFileMD5(fileMD5); err != nil {
		return nil, err
	}

	key := fmt.Sprintf("upload:chunks:%s", fileMD5)

	status, _ := s.rdb.Get(ctx, key+":status").Result()
	if status == "COMPLETED" {
		return map[string]any{"status": "completed", "uploaded": []int{}}, nil
	}

	uploaded, err := s.rdb.SMembers(ctx, key).Result()
	if err != nil {
		return map[string]any{"status": "new", "uploaded": []int{}}, nil
	}

	nums := make([]int, 0, len(uploaded))
	for _, v := range uploaded {
		n, _ := strconv.Atoi(v)
		nums = append(nums, n)
	}
	return map[string]any{"status": "uploading", "uploaded": nums}, nil
}

// UploadChunk 先落盘后记账
func (s *MediaService) UploadChunk(ctx context.Context, fileMD5 string, chunkNumber int, chunkData []byte, chunkSize int64) error {
	if err := validateFileMD5(fileMD5); err != nil {
		return err
	}
	if s.cfg.ChunkSize > 0 && chunkSize > s.cfg.ChunkSize {
		return fmt.Errorf("分片大小超过限制: 最大 %d 字节", s.cfg.ChunkSize)
	}

	objectName := fmt.Sprintf("chunks/%s/%d", fileMD5, chunkNumber)
	if err := s.storage.UploadFile(ctx, objectName, &readerWrapper{data: chunkData}, chunkSize, "application/octet-stream"); err != nil {
		return fmt.Errorf("分片落盘失败: %w", err)
	}
	key := fmt.Sprintf("upload:chunks:%s", fileMD5)
	s.rdb.SAdd(ctx, key, chunkNumber)
	s.rdb.Expire(ctx, key, 24*time.Hour)
	return nil
}

// MergeChunks 合并分片
func (s *MediaService) MergeChunks(ctx context.Context, userID int64, fileMD5, filename string, totalChunks int) (*UploadResult, error) {
	if err := validateFileMD5(fileMD5); err != nil {
		return nil, err
	}
	if totalChunks <= 0 {
		return nil, fmt.Errorf("total_chunks 必须大于 0")
	}

	existingAsset, err := s.repo.Asset.FindByMD5(fileMD5)
	if err != nil {
		return nil, err
	}
	if existingAsset != nil {
		return s.createTaskFromAsset(userID, filename, existingAsset, model.TaskStatusPending)
	}

	mergeLock := lock.NewRedisLock(s.rdb, fmt.Sprintf("vidlens:merge:%s", fileMD5))
	acquired, err := mergeLock.TryLock(ctx, 0)
	if err != nil || !acquired {
		existingAsset, findErr := s.repo.Asset.FindByMD5(fileMD5)
		if findErr == nil && existingAsset != nil {
			return s.createTaskFromAsset(userID, filename, existingAsset, model.TaskStatusPending)
		}
		return nil, fmt.Errorf("合并操作正在进行中，请稍后")
	}
	defer mergeLock.Unlock(ctx)

	key := fmt.Sprintf("upload:chunks:%s", fileMD5)
	for i := range totalChunks {
		exists, err := s.rdb.SIsMember(ctx, key, i).Result()
		if err != nil {
			return nil, fmt.Errorf("检查分片状态失败: %w", err)
		}
		if !exists {
			return nil, fmt.Errorf("分片未全部上传完成: 缺少第 %d 片", i)
		}
	}

	ext := filepath.Ext(filename)
	dst := fmt.Sprintf("videos/%s%s", uuid.New().String(), ext)

	srcs := make([]minio.CopySrcOptions, 0, totalChunks)
	for i := range totalChunks {
		srcs = append(srcs, minio.CopySrcOptions{
			Bucket: s.storage.BucketName(),
			Object: fmt.Sprintf("chunks/%s/%d", fileMD5, i),
		})
	}

	size, err := s.storage.ComposeObject(ctx, dst, srcs)
	if err != nil {
		return nil, fmt.Errorf("合并分片失败: %w", err)
	}

	asset := &model.VideoAsset{
		FileMD5:     fileMD5,
		ObjectName:  dst,
		FileSize:    size,
		ContentType: contentTypeForFilename(filename),
	}
	if err := s.repo.Asset.Create(asset); err != nil {
		_ = s.storage.DeleteObject(ctx, dst)
		return nil, err
	}

	s.rdb.Set(ctx, key+":status", "COMPLETED", 24*time.Hour)
	// 合并产物已落库，清理分片对象与 Redis 分片状态（best-effort，失败不影响合并结果）。
	// status=COMPLETED 保留，供 check-upload 识别已完成上传。
	s.cleanupMergedChunks(ctx, fileMD5, totalChunks)
	return s.createTaskFromAsset(userID, filename, asset, model.TaskStatusPending)
}

// cleanupMergedChunks 合并成功后清理 MinIO 分片对象与 Redis 分片状态。
// best-effort：单个清理失败仅记录日志，不阻塞已成功的合并（分片残留只浪费少量存储，不影响业务正确性）。
func (s *MediaService) cleanupMergedChunks(ctx context.Context, fileMD5 string, totalChunks int) {
	for i := range totalChunks {
		objName := fmt.Sprintf("chunks/%s/%d", fileMD5, i)
		if err := s.deleteObject(ctx, objName); err != nil {
			log.Printf("[media] 清理分片对象失败（可忽略）: %s err=%v", objName, err)
		}
	}
	key := fmt.Sprintf("upload:chunks:%s", fileMD5)
	s.rdb.Del(ctx, key, key+":total")
}

// readerWrapper []byte → io.Reader
type readerWrapper struct {
	data   []byte
	offset int
}

func (r *readerWrapper) Read(p []byte) (n int, err error) {
	if r.offset >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.offset:])
	r.offset += n
	return
}
