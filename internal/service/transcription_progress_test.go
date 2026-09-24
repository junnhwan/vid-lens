package service

import (
	"context"
	"testing"
	"time"

	"vid-lens/internal/config"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

func TestGetTranscriptionProgressCountsChunksAndProtectsText(t *testing.T) {
	repos := newMediaTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Filename: "long.mp4", FileURL: "videos/long.mp4", Status: model.TaskStatusRunning, Stage: model.TaskStageTranscribing}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	first := repository.TranscriptionChunkTimeline{WindowStartMS: 0, WindowEndMS: 300_000, CoreStartMS: 0, CoreEndMS: 300_000}
	second := repository.TranscriptionChunkTimeline{WindowStartMS: 295_000, WindowEndMS: 600_000, CoreStartMS: 300_000, CoreEndMS: 600_000}
	if err := repos.TranscriptionChunk.UpsertCompletedWithTimeline(task.ID, 0, "private-object", "已转文字", first); err != nil {
		t.Fatal(err)
	}
	if err := repos.TranscriptionChunk.UpsertPendingWithTimeline(task.ID, 1, "private-object", second); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(time.Minute)
	if err := repos.TranscriptionChunk.MarkRetryWait(task.ID, 1, 1, "provider_rate_limit", until); err != nil {
		t.Fatal(err)
	}
	svc := &MediaService{repo: repos, transcriptionMQ: config.MQConfig{Prefetch: 1, TranscribePrefetch: 2, ASRConcurrency: 3}}
	if _, err := svc.GetTranscriptionProgress(context.Background(), 8, task.ID); err == nil {
		t.Fatal("other user read transcription progress")
	}
	got, err := svc.GetTranscriptionProgress(context.Background(), 7, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 2 || got.Completed != 1 || got.RetryWaiting != 1 || got.VideoConcurrency != 2 || got.ChunkConcurrency != 3 {
		t.Fatalf("progress = %+v", got)
	}
	if got.Chunks[0].Content != "已转文字" || got.Chunks[1].Content != "" || got.Chunks[1].StartMS != 300_000 || got.Chunks[1].WaitReason != "provider_rate_limit" {
		t.Fatalf("chunks = %+v", got.Chunks)
	}
}
