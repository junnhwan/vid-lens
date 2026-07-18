package mq

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"vid-lens/internal/model"
	"vid-lens/internal/observability"
	"vid-lens/internal/pkg/ytdlp"
	"vid-lens/internal/repository"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
)

func (c *Consumer) handleDownload(ctx context.Context, msg kafka.Message) error {
	var payload DownloadPayload
	if err := json.Unmarshal(msg.Value, &payload); err != nil {
		return fmt.Errorf("解析下载消息失败: %w", err)
	}

	task, err := c.repo.Task.FindByID(payload.TaskID)
	if err != nil {
		return fmt.Errorf("查询下载任务失败: %w", err)
	}
	traceID := traceIDForTask(payload.TraceID, task)
	claim, err := c.claimTaskForMessage(task.ID, TaskJobDownload, model.TaskStageDownloading, payload.ClaimToken)
	if err != nil {
		return fmt.Errorf("获取下载 processing lease 失败: %w", err)
	}
	switch claim.Outcome {
	case repository.TaskLeaseBusy:
		return fmt.Errorf("下载 processing lease 正由其他消费者持有")
	case repository.TaskLeaseStale, repository.TaskLeaseTerminal:
		return nil
	case repository.TaskLeaseAcquired:
	default:
		return fmt.Errorf("未知下载 processing lease 状态: %s", claim.Outcome)
	}
	ctx, stopLease := c.startProcessingLeaseHeartbeat(ctx, task.ID, TaskJobDownload, claim.Token)
	defer stopLease()
	task.TraceID = traceID
	ctx = c.contextForTaskJob(ctx, task, TaskJobDownload, payload.BudgetID)
	if strings.TrimSpace(task.SourceURL) == "" {
		return c.recordTaskFailure(task.ID, TaskJobDownload, model.TaskStageDownloading, fmt.Errorf("URL 下载任务缺少 source_url"), claim.Token)
	}

	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	observability.Log(ctx, slog.Default(), slog.LevelInfo, "video download started")
	localPath, err := c.callDownloadVideo(ctx, task.SourceURL)
	if err != nil {
		errMsg := truncateError(err)
		observability.Log(ctx, slog.Default(), slog.LevelError, "video download failed", slog.String("error", observability.SafeError(err)))
		return c.recordTaskFailure(task.ID, TaskJobDownload, model.TaskStageDownloading, errors.New(errMsg), claim.Token)
	}
	defer os.Remove(localPath)

	fileMD5, size, err := hashLocalFile(localPath)
	if err != nil {
		return c.recordTaskFailure(task.ID, TaskJobDownload, model.TaskStageDownloading, err, claim.Token)
	}

	asset, err := c.repo.Asset.FindByMD5(fileMD5)
	if err != nil {
		return c.recordTaskFailure(task.ID, TaskJobDownload, model.TaskStageDownloading, err, claim.Token)
	}
	if asset == nil {
		objectName := fmt.Sprintf("videos/%s%s", uuid.New().String(), extensionForDownloadedFile(localPath))
		if err := c.callUploadLocalFile(ctx, localPath, objectName, "video/mp4"); err != nil {
			return c.recordTaskFailure(task.ID, TaskJobDownload, model.TaskStageDownloading, fmt.Errorf("上传到 MinIO 失败: %w", err), claim.Token)
		}
		asset = &model.VideoAsset{
			FileMD5:     fileMD5,
			ObjectName:  objectName,
			FileSize:    size,
			ContentType: "video/mp4",
		}
		if err := c.repo.Asset.Create(asset); err != nil {
			existing, findErr := c.repo.Asset.FindByMD5(fileMD5)
			if findErr == nil && existing != nil {
				asset = existing
			} else {
				return c.recordTaskFailure(task.ID, TaskJobDownload, model.TaskStageDownloading, err, claim.Token)
			}
		}
	}

	filename := "WEB_" + filepath.Base(localPath)
	if filename == "WEB_" || filename == "WEB_." {
		filename = task.Filename
	}
	completed, err := c.completeTaskProcessing(repository.TaskProcessingCompleteRequest{
		TaskID: task.ID, JobType: TaskJobDownload, JobStage: model.TaskStageDownloading, Token: claim.Token,
		TaskStatus: model.TaskStatusPending, TaskStage: model.TaskStageUploaded, Now: c.currentTime(),
		TaskFields: map[string]interface{}{"asset_id": asset.ID, "file_md5": asset.FileMD5, "filename": filename, "file_url": asset.ObjectName, "file_size": asset.FileSize, "last_job_type": ""},
	})
	if err != nil {
		return fmt.Errorf("回写下载任务失败: %w", err)
	}
	if !completed {
		return nil
	}
	observability.Log(ctx, slog.Default(), slog.LevelInfo, "video download completed", slog.Int64("asset_id", asset.ID), slog.Int64("file_size", asset.FileSize))
	return nil
}

func (c *Consumer) callDownloadVideo(ctx context.Context, sourceURL string) (string, error) {
	if c.downloadVideo != nil {
		return c.downloadVideo(ctx, sourceURL)
	}
	sanitizedURL, err := c.validateDownloadURL(ctx, sourceURL)
	if err != nil {
		return "", err
	}
	return ytdlp.DownloadVideo(ctx, c.ytdlpPath, c.ffmpegPath, c.cookiesPath, c.proxyURL, sanitizedURL)
}

func (c *Consumer) validateDownloadURL(ctx context.Context, sourceURL string) (string, error) {
	checked, err := c.downloadURLPolicy.Validate(ctx, sourceURL)
	if err != nil {
		return "", fmt.Errorf("下载 URL 安全校验失败: %w", err)
	}
	return checked.Sanitized, nil
}

func (c *Consumer) callUploadLocalFile(ctx context.Context, localPath, objectName, contentType string) error {
	if c.uploadLocalFile != nil {
		return c.uploadLocalFile(ctx, localPath, objectName, contentType)
	}
	return fmt.Errorf("对象存储未初始化")
}

func hashLocalFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("打开下载文件失败: %w", err)
	}
	defer file.Close()

	hasher := md5.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return "", 0, fmt.Errorf("计算下载文件 MD5 失败: %w", err)
	}
	if size == 0 {
		return "", 0, fmt.Errorf("下载文件为空")
	}
	return hex.EncodeToString(hasher.Sum(nil)), size, nil
}

func extensionForDownloadedFile(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".avi", ".mkv", ".mov", ".webm", ".mp4":
		return ext
	default:
		return ".mp4"
	}
}
