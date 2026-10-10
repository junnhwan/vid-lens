package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/ytdlp"
)

func (c *Consumer) downloadFrozenBilibili(ctx context.Context, task *model.VideoTask, token string) (ytdlp.DownloadedVideo, error) {
	adapter, err := ytdlp.NewAdapter(ytdlp.Config{YtDlpPath: c.ytdlpPath, FFmpegPath: c.ffmpegPath, CookiesPath: c.cookiesPath, ProxyURL: c.proxyURL})
	if err != nil {
		return ytdlp.DownloadedVideo{}, err
	}
	job, err := c.repo.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeDownload)
	if err != nil {
		return ytdlp.DownloadedVideo{}, err
	}
	var identity ytdlp.BilibiliIdentity
	if job != nil && job.InputSnapshotJSON != "" {
		if err = json.Unmarshal([]byte(job.InputSnapshotJSON), &identity); err != nil {
			return ytdlp.DownloadedVideo{}, fmt.Errorf("invalid frozen download identity")
		}
	} else {
		resolver := textSourceSubtitleAdapter(adapter)
		if c.textSourceAdapter != nil {
			resolver = c.textSourceAdapter
		}
		identity, err = resolver.ResolveIdentity(ctx, task.SourceURL)
		if err != nil {
			return ytdlp.DownloadedVideo{}, err
		}
		raw, err := json.Marshal(identity)
		if err != nil {
			return ytdlp.DownloadedVideo{}, err
		}
		if err = c.repo.FreezeDownloadIdentity(ctx, task.ID, token, string(raw)); err != nil {
			return ytdlp.DownloadedVideo{}, err
		}
	}
	if c.downloadIdentity != nil {
		return c.downloadIdentity(ctx, identity)
	}
	return adapter.DownloadVideoWithIdentity(ctx, identity)
}
