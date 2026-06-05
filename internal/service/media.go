package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
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
	"vid-lens/internal/pkg/ytdlp"
	"vid-lens/internal/repository"
	"vid-lens/internal/storage"
)

type MediaService struct {
	repo    *repository.Repositories
	storage *storage.MinIOStorage
	mq      *mq.Producer
	rdb     redis.Cmdable
	cfg     config.UploadConfig
	tools   config.ToolsConfig
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
		repo:    repo,
		storage: storage,
		mq:      mqProducer,
		rdb:     rdb,
		cfg:     cfg,
		tools:   tools,
	}
}

type UploadResult struct {
	TaskID   int64  `json:"task_id"`
	FileMD5  string `json:"file_md5"`
	Filename string `json:"filename"`
	FileURL  string `json:"file_url"`
	FileSize int64  `json:"file_size"`
}

// UploadFile 普通文件上传
func (s *MediaService) UploadFile(ctx context.Context, userID int64, filename string, fileStream io.Reader, fileSize int64) (*UploadResult, error) {
	data, err := io.ReadAll(fileStream)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	hash := md5.Sum(data)
	fileMD5 := hex.EncodeToString(hash[:])

	// 内容级去重
	existing, _ := s.repo.Task.FindByMD5(fileMD5)
	if existing != nil && existing.Status == model.TaskStatusCompleted {
		task := &model.VideoTask{
			UserID: userID, FileMD5: fileMD5, Filename: filename,
			FileURL: existing.FileURL, FileSize: existing.FileSize,
			Status: model.TaskStatusCompleted,
		}
		if err := s.repo.Task.Create(task); err != nil {
			return nil, err
		}
		return &UploadResult{
			TaskID: task.ID, FileMD5: fileMD5, Filename: filename,
			FileURL: existing.FileURL, FileSize: existing.FileSize,
		}, nil
	}

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
		UserID: userID, FileMD5: fileMD5, Filename: filename,
		FileURL: objectName, FileSize: int64(len(data)), Status: model.TaskStatusPending,
	}
	if err := s.repo.Task.Create(task); err != nil {
		return nil, err
	}

	return &UploadResult{
		TaskID: task.ID, FileMD5: fileMD5, Filename: filename,
		FileURL: objectName, FileSize: int64(len(data)),
	}, nil
}

// UploadByURL 通过 URL 下载视频并上传
func (s *MediaService) UploadByURL(ctx context.Context, userID int64, videoURL string) (*UploadResult, error) {
	localPath, err := ytdlp.DownloadVideo(ctx, s.tools.YtDlpPath, s.tools.FFmpegPath, videoURL)
	if err != nil {
		return nil, fmt.Errorf("视频下载失败: %w", err)
	}
	defer os.Remove(localPath)

	fileData, err := os.ReadFile(localPath)
	if err != nil {
		return nil, fmt.Errorf("读取下载文件失败: %w", err)
	}
	hash := md5.Sum(fileData)
	fileMD5 := hex.EncodeToString(hash[:])

	// 去重
	existing, _ := s.repo.Task.FindByMD5(fileMD5)
	if existing != nil && existing.Status == model.TaskStatusCompleted {
		task := &model.VideoTask{
			UserID: userID, FileMD5: fileMD5,
			Filename: "WEB_" + filepath.Base(localPath),
			FileURL: existing.FileURL, FileSize: existing.FileSize,
			Status: model.TaskStatusCompleted,
		}
		s.repo.Task.Create(task)
		return &UploadResult{
			TaskID: task.ID, FileMD5: fileMD5,
			Filename: task.Filename, FileURL: existing.FileURL,
			FileSize: existing.FileSize,
		}, nil
	}

	// 上传到 MinIO（UploadFromPath 返回 fileSize 和 error）
	objectName := fmt.Sprintf("videos/%s.mp4", uuid.New().String())
	if _, err := s.storage.UploadFromPath(ctx, localPath, objectName, "video/mp4"); err != nil {
		return nil, fmt.Errorf("上传到 MinIO 失败: %w", err)
	}

	task := &model.VideoTask{
		UserID: userID, FileMD5: fileMD5,
		Filename: "WEB_" + filepath.Base(localPath),
		FileURL: objectName, FileSize: int64(len(fileData)),
		Status: model.TaskStatusPending,
	}
	if err := s.repo.Task.Create(task); err != nil {
		return nil, err
	}

	return &UploadResult{
		TaskID: task.ID, FileMD5: fileMD5, Filename: task.Filename,
		FileURL: objectName, FileSize: int64(len(fileData)),
	}, nil
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
		return fmt.Errorf("任务已完成，可直接查看结果")
	}

	if err := s.repo.Task.UpdateStatus(taskID, model.TaskStatusQueued, ""); err != nil {
		return err
	}
	if err := s.mq.EnqueueAnalyze(ctx, taskID, task.FileMD5); err != nil {
		s.repo.Task.UpdateStatus(taskID, model.TaskStatusPending, "消息投递失败")
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

	s.repo.Task.UpdateStatus(taskID, model.TaskStatusQueued, "")
	if err := s.mq.EnqueueTranscribe(ctx, taskID, task.FileMD5); err != nil {
		s.repo.Task.UpdateStatus(taskID, model.TaskStatusPending, "消息投递失败")
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

// ListTasks 分页查询
func (s *MediaService) ListTasks(userID int64, page, pageSize int) ([]model.VideoTask, int64, error) {
	return s.repo.Task.ListByUserID(userID, page, pageSize)
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
	if task.FileURL != "" {
		s.storage.DeleteObject(ctx, task.FileURL)
	}
	return s.repo.Task.Delete(taskID)
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

func (s *MediaService) CheckUploadProgress(ctx context.Context, fileMD5 string) (map[string]interface{}, error) {
	key := fmt.Sprintf("upload:chunks:%s", fileMD5)

	status, _ := s.rdb.Get(ctx, key+":status").Result()
	if status == "COMPLETED" {
		return map[string]interface{}{"status": "completed", "uploaded": []int{}}, nil
	}

	uploaded, err := s.rdb.SMembers(ctx, key).Result()
	if err != nil {
		return map[string]interface{}{"status": "new", "uploaded": []int{}}, nil
	}

	nums := make([]int, 0, len(uploaded))
	for _, v := range uploaded {
		n, _ := strconv.Atoi(v)
		nums = append(nums, n)
	}
	return map[string]interface{}{"status": "uploading", "uploaded": nums}, nil
}

// UploadChunk 先落盘后记账
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
func (s *MediaService) MergeChunks(ctx context.Context, userID int64, fileMD5, filename string, totalChunks int) (*UploadResult, error) {
	mergeLock := lock.NewRedisLock(s.rdb, fmt.Sprintf("vidlens:merge:%s", fileMD5))
	acquired, err := mergeLock.TryLock(ctx, 0)
	if err != nil || !acquired {
		existing, _ := s.repo.Task.FindByMD5(fileMD5)
		if existing != nil {
			return &UploadResult{
				TaskID: existing.ID, FileMD5: fileMD5, Filename: filename,
				FileURL: existing.FileURL, FileSize: existing.FileSize,
			}, nil
		}
		return nil, fmt.Errorf("合并操作正在进行中，请稍后")
	}
	defer mergeLock.Unlock(ctx)

	key := fmt.Sprintf("upload:chunks:%s", fileMD5)
	cnt, _ := s.rdb.SCard(ctx, key).Result()
	if int(cnt) < totalChunks {
		return nil, fmt.Errorf("分片未全部上传完成: 已传 %d, 需传 %d", cnt, totalChunks)
	}

	ext := filepath.Ext(filename)
	dst := fmt.Sprintf("videos/%s%s", uuid.New().String(), ext)

	srcs := make([]minio.CopySrcOptions, 0, totalChunks)
	for i := 0; i < totalChunks; i++ {
		srcs = append(srcs, minio.CopySrcOptions{
			Bucket: s.storage.BucketName(),
			Object: fmt.Sprintf("chunks/%s/%d", fileMD5, i),
		})
	}

	if err := s.storage.ComposeObject(ctx, dst, srcs); err != nil {
		return nil, fmt.Errorf("合并分片失败: %w", err)
	}

	task := &model.VideoTask{
		UserID: userID, FileMD5: fileMD5, Filename: filename,
		FileURL: dst, Status: model.TaskStatusPending,
	}
	if err := s.repo.Task.Create(task); err != nil {
		return nil, err
	}

	s.rdb.Set(ctx, key+":status", "COMPLETED", 24*time.Hour)
	return &UploadResult{TaskID: task.ID, FileMD5: fileMD5, Filename: filename, FileURL: dst}, nil
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
