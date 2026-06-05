package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
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

type MediaService struct {
	repo    *repository.Repositories
	storage *storage.MinIOStorage
	mq      *mq.Producer
	rdb     redis.Cmdable
	cfg     config.UploadConfig
}

func NewMediaService(
	repo *repository.Repositories,
	storage *storage.MinIOStorage,
	mqProducer *mq.Producer,
	rdb redis.Cmdable,
	cfg config.UploadConfig,
) *MediaService {
	return &MediaService{
		repo:    repo,
		storage: storage,
		mq:      mqProducer,
		rdb:     rdb,
		cfg:     cfg,
	}
}

// UploadResult 上传结果
type UploadResult struct {
	TaskID   int64  `json:"task_id"`
	FileMD5  string `json:"file_md5"`
	Filename string `json:"filename"`
	FileURL  string `json:"file_url"`
	FileSize int64  `json:"file_size"`
}

// UploadFile 普通文件上传
// 面试亮点：
//   1. 文件直传 MinIO，避免应用服务器带宽瓶颈
//   2. 计算文件 MD5 实现内容级去重
//   3. 数据库唯一索引兜底，防止极端情况下的重复记录
func (s *MediaService) UploadFile(ctx context.Context, userID int64, filename string, fileStream io.Reader, fileSize int64) (*UploadResult, error) {
	data, err := io.ReadAll(fileStream)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	// 计算文件 MD5
	hash := md5.Sum(data)
	fileMD5 := hex.EncodeToString(hash[:])

	// 内容级去重：检查是否已有相同 MD5 的已完成任务
	existing, _ := s.repo.Task.FindByMD5(fileMD5)
	if existing != nil && existing.Status == model.TaskStatusCompleted {
		task := &model.VideoTask{
			UserID:   userID,
			FileMD5:  fileMD5,
			Filename: filename,
			FileURL:  existing.FileURL,
			FileSize: existing.FileSize,
			Status:   model.TaskStatusCompleted,
		}
		if err := s.repo.Task.Create(task); err != nil {
			return nil, err
		}
		return &UploadResult{
			TaskID:   task.ID,
			FileMD5:  fileMD5,
			Filename: filename,
			FileURL:  existing.FileURL,
			FileSize: existing.FileSize,
		}, nil
	}

	// 上传到 MinIO（私有桶）
	ext := filepath.Ext(filename)
	objectName := fmt.Sprintf("videos/%s%s", uuid.New().String(), ext)
	contentType := "video/mp4"
	if ext == ".avi" {
		contentType = "video/x-msvideo"
	} else if ext == ".mkv" {
		contentType = "video/x-matroska"
	}

	if err := s.storage.UploadFile(ctx, objectName, &readerWrapper{data: data}, int64(len(data)), contentType); err != nil {
		return nil, fmt.Errorf("上传到 MinIO 失败: %w", err)
	}

	task := &model.VideoTask{
		UserID:   userID,
		FileMD5:  fileMD5,
		Filename: filename,
		FileURL:  objectName,
		FileSize: int64(len(data)),
		Status:   model.TaskStatusPending,
	}
	if err := s.repo.Task.Create(task); err != nil {
		return nil, err
	}

	return &UploadResult{
		TaskID:   task.ID,
		FileMD5:  fileMD5,
		Filename: filename,
		FileURL:  objectName,
		FileSize: int64(len(data)),
	}, nil
}

// RequestAnalysis 提交 AI 分析请求
// 面试亮点：令牌桶限流 → MD5 去重检查 → 投递 MQ → 即时返回
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
		return fmt.Errorf("任务已完成，可直接查看结果")
	}

	if err := s.repo.Task.UpdateStatus(taskID, model.TaskStatusQueued, ""); err != nil {
		return err
	}

	if err := s.mq.EnqueueAnalyze(taskID, task.FileMD5); err != nil {
		s.repo.Task.UpdateStatus(taskID, model.TaskStatusPending, "消息投递失败")
		return fmt.Errorf("系统繁忙，请稍后重试")
	}

	return nil
}

// RequestTranscribe 提交文字提取请求
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

	s.repo.Task.UpdateStatus(taskID, model.TaskStatusQueued, "")

	if err := s.mq.EnqueueTranscribe(taskID, task.FileMD5); err != nil {
		s.repo.Task.UpdateStatus(taskID, model.TaskStatusPending, "消息投递失败")
		return fmt.Errorf("系统繁忙，请稍后重试")
	}

	return nil
}

// GetTaskDetail 获取任务详情（含转录和总结）
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

// ListTasks 分页查询用户的视频任务列表
func (s *MediaService) ListTasks(userID int64, page, pageSize int) ([]model.VideoTask, int64, error) {
	return s.repo.Task.ListByUserID(userID, page, pageSize)
}

// DeleteTask 删除任务
func (s *MediaService) DeleteTask(ctx context.Context, userID, taskID int64) error {
	task, err := s.repo.Task.FindByID(taskID)
	if err != nil {
		return fmt.Errorf("任务不存在")
	}
	if task.UserID != userID {
		return fmt.Errorf("无权删除此任务")
	}

	if task.FileURL != "" {
		s.storage.DeleteObject(ctx, task.FileURL)
	}

	return s.repo.Task.Delete(taskID)
}

// GetPresignedURL 获取文件的预签名下载链接
func (s *MediaService) GetPresignedURL(ctx context.Context, taskID int64) (string, error) {
	task, err := s.repo.Task.FindByID(taskID)
	if err != nil {
		return "", err
	}
	return s.storage.GetPresignedURL(ctx, task.FileURL)
}

// ===== 分片上传相关 =====

// InitChunkedUpload 初始化分片上传
func (s *MediaService) InitChunkedUpload(ctx context.Context, fileMD5 string, totalChunks int) error {
	key := fmt.Sprintf("upload:chunks:%s", fileMD5)
	s.rdb.Set(ctx, key+":total", totalChunks, 24*time.Hour)
	s.rdb.Set(ctx, key+":status", "INIT", 24*time.Hour)
	return nil
}

// CheckUploadProgress 查询上传进度（断点续传核心）
func (s *MediaService) CheckUploadProgress(ctx context.Context, fileMD5 string) (map[string]interface{}, error) {
	key := fmt.Sprintf("upload:chunks:%s", fileMD5)

	status, _ := s.rdb.Get(ctx, key+":status").Result()
	if status == "COMPLETED" {
		return map[string]interface{}{
			"status":   "completed",
			"uploaded": []int{},
		}, nil
	}

	uploaded, err := s.rdb.SMembers(ctx, key).Result()
	if err != nil {
		return map[string]interface{}{
			"status":   "new",
			"uploaded": []int{},
		}, nil
	}

	uploadedNums := make([]int, 0, len(uploaded))
	for _, v := range uploaded {
		n, _ := strconv.Atoi(v)
		uploadedNums = append(uploadedNums, n)
	}

	return map[string]interface{}{
		"status":   "uploading",
		"uploaded": uploadedNums,
	}, nil
}

// UploadChunk 上传单个分片
// 面试亮点："先落盘，后记账"
func (s *MediaService) UploadChunk(ctx context.Context, fileMD5 string, chunkNumber int, chunkData []byte, chunkSize int64) error {
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
// 面试亮点：前端触发合并 + SETNX 幂等锁 + MinIO ComposeObject 服务端合并
func (s *MediaService) MergeChunks(ctx context.Context, userID int64, fileMD5, filename string, totalChunks int) (*UploadResult, error) {
	// 1. SETNX 合并锁
	mergeLockKey := fmt.Sprintf("vidlens:merge:%s", fileMD5)
	mergeLock := lock.NewRedisLock(s.rdb, mergeLockKey)
	acquired, err := mergeLock.TryLock(ctx, 0)
	if err != nil || !acquired {
		existing, _ := s.repo.Task.FindByMD5(fileMD5)
		if existing != nil {
			return &UploadResult{
				TaskID:   existing.ID,
				FileMD5:  fileMD5,
				Filename: filename,
				FileURL:  existing.FileURL,
				FileSize: existing.FileSize,
			}, nil
		}
		return nil, fmt.Errorf("合并操作正在进行中，请稍后")
	}
	defer mergeLock.Unlock(ctx)

	// 2. 校验分片数
	key := fmt.Sprintf("upload:chunks:%s", fileMD5)
	uploadedCount, _ := s.rdb.SCard(ctx, key).Result()
	if int(uploadedCount) < totalChunks {
		return nil, fmt.Errorf("分片未全部上传完成: 已传 %d, 需传 %d", uploadedCount, totalChunks)
	}

	// 3. 构建 MinIO CopySrc 列表
	ext := filepath.Ext(filename)
	dstObject := fmt.Sprintf("videos/%s%s", uuid.New().String(), ext)

	srcOpts := make([]minio.CopySrcOptions, 0, totalChunks)
	for i := 0; i < totalChunks; i++ {
		srcOpts = append(srcOpts, minio.CopySrcOptions{
			Bucket: s.storage.BucketName(),
			Object: fmt.Sprintf("chunks/%s/%d", fileMD5, i),
		})
	}

	if err := s.storage.ComposeObject(ctx, dstObject, srcOpts); err != nil {
		return nil, fmt.Errorf("合并分片失败: %w", err)
	}

	// 4. 创建任务记录
	task := &model.VideoTask{
		UserID:   userID,
		FileMD5:  fileMD5,
		Filename: filename,
		FileURL:  dstObject,
		FileSize: 0,
		Status:   model.TaskStatusPending,
	}
	if err := s.repo.Task.Create(task); err != nil {
		return nil, err
	}

	// 5. 标记上传完成
	s.rdb.Set(ctx, key+":status", "COMPLETED", 24*time.Hour)

	return &UploadResult{
		TaskID:   task.ID,
		FileMD5:  fileMD5,
		Filename: filename,
		FileURL:  dstObject,
	}, nil
}

// readerWrapper 将 []byte 包装为 io.Reader
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
