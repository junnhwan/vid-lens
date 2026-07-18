package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	neturl "net/url"
	"strings"

	"vid-lens/internal/model"

	"github.com/google/uuid"
)

// UploadByURL 只创建下载任务并立即返回，实际下载由 Kafka consumer 异步执行。
func (s *MediaService) UploadByURL(ctx context.Context, userID int64, videoURL string) (*UploadResult, error) {
	checkedURL, err := newRemoteVideoURLValidator(s.tools, s.remoteURLResolver).validate(ctx, videoURL)
	if err != nil {
		return nil, err
	}

	key := md5HexString(checkedURL.Sanitized)
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
	}
	_, err = s.enqueueInitialTask(ctx, task, initialDispatchSpec{
		createTask: true,
		jobType:    model.TaskJobTypeDownload,
		stage:      model.TaskStageDownloading,
		enqueue: func(enqueueCtx context.Context, prepared model.VideoTask) error {
			return s.mq.EnqueueDownload(enqueueCtx, prepared.ID, key)
		},
	})
	if err != nil {
		return nil, publicInitialDispatchError(ctx, *task, model.TaskJobTypeDownload, model.TaskStageDownloading, err)
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
