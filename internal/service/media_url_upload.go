package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	neturl "net/url"
	"strings"

	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/remoteurl"
	"vid-lens/internal/repository"

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
		VisualMode: model.VisualModeOff, VisualDisabled: true,
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

// UploadByURLWithOptions accepts the summary-first import contract.
func (s *MediaService) UploadByURLWithOptions(ctx context.Context, userID int64, videoURL string, options ImportOptions) (*UploadResult, error) {
	if !options.AutoSummary {
		return s.UploadByURL(ctx, userID, videoURL)
	}
	parsed, err := neturl.Parse(strings.TrimSpace(videoURL))
	if err != nil || parsed.User != nil {
		return nil, artifact.Err("unsupported_import_url", 400)
	}
	sanitized, err := remoteurl.SanitizeChecked(*parsed)
	if err != nil {
		return nil, artifact.Err("invalid_import_url", 400)
	}
	request, replay, err := s.lookupImport(ctx, userID, "upload_url", sanitized, options, false)
	if err != nil || replay != nil {
		return replay, err
	}
	if !remoteurl.HostAllowed(parsed.Hostname(), []string{"bilibili.com", "b23.tv"}) {
		return nil, artifact.Err("unsupported_import_url", 400)
	}
	checked, err := newRemoteVideoURLValidator(s.tools, s.remoteURLResolver).validate(ctx, videoURL)
	if err != nil {
		return nil, artifact.Err("invalid_import_url", 400)
	}
	if err = s.freezeImport(userID, request); err != nil {
		return nil, err
	}
	key := md5HexString(checked.Sanitized)
	return s.acceptImport(ctx, userID, request, model.TaskJobTypeDownload, model.TaskStageDownloading, func(*repository.Repositories) (*model.VideoTask, error) {
		return &model.VideoTask{UserID: userID, FileMD5: key, Filename: filenameForURLTask(checked.Sanitized), Status: model.TaskStatusRunning, Stage: model.TaskStageDownloading, TraceID: uuid.NewString(), SourceType: model.TaskSourceTypeURL, SourceURL: checked.Sanitized, MaxRetries: 3}, nil
	}, func(ctx context.Context, task model.VideoTask) error { return s.mq.EnqueueDownload(ctx, task.ID, key) })
}
