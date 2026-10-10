package mq

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"vid-lens/internal/model"
	"vid-lens/internal/pkg/ytdlp"
	"vid-lens/internal/processing"
	"vid-lens/internal/repository"
	"vid-lens/internal/textsource"
)

func TestAutomaticDownloadFreezesIdentityAndDurablyHandsOffSource(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	ctx := context.Background()
	options, _ := processing.Normalize(processing.Options{AutoSummary: true}, false)
	intentJSON, _ := json.Marshal(processing.Intent{ID: "import-1", Version: 1, Options: options, GenerationID: "generation-1", RecipeVersion: processing.Recipe})
	task := model.VideoTask{UserID: 7, FileMD5: "11111111111111111111111111111111", Filename: "WEB_pending.mp4", Stage: model.TaskStageDownloading, SourceType: model.TaskSourceTypeURL, SourceURL: "https://www.bilibili.com/video/BV1xx411c7mD?p=2", ProcessingIntentJSON: string(intentJSON)}
	now := time.Now()
	dispatch, err := repos.PrepareInitialTaskDispatch(repository.InitialTaskDispatchRequest{Task: &task, CreateTask: true, JobType: model.TaskJobTypeDownload, Stage: model.TaskStageDownloading, Token: "download-dispatch", Now: now, LeaseUntil: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	adapter := &sourceFixtureAdapter{}
	producer := &sourceFixtureProducer{err: errors.New("MQ unavailable")}
	c := NewConsumer(repos, nil, nil, nil, "ffmpeg")
	c.SetTextSourceAdapter(adapter)
	c.SetSourceDispatchProducer(producer)
	downloads := 0
	c.downloadIdentity = func(ctx context.Context, id ytdlp.BilibiliIdentity) (ytdlp.DownloadedVideo, error) {
		downloads++
		if id.PartIndex != 2 || id.CID != 222 {
			t.Fatalf("wrong identity=%+v", id)
		}
		job, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeDownload)
		if job.InputSnapshotJSON == "" {
			t.Fatal("identity not durable before transfer")
		}
		path := filepath.Join(t.TempDir(), "downloaded.mp4")
		if err := os.WriteFile(path, []byte("verified media"), 0600); err != nil {
			t.Fatal(err)
		}
		return ytdlp.DownloadedVideo{Path: path, Identity: id, IdentityBasis: "fixture"}, nil
	}
	c.uploadLocalFile = func(context.Context, string, string, string) error { return nil }
	body, _ := json.Marshal(DownloadPayload{TaskID: task.ID, ClaimToken: dispatch.Token, BudgetID: dispatch.RetryBudgetID})
	delivery := downloadMessage(task.ID, task.FileMD5)
	delivery.Body = body
	if err := c.handleDownload(ctx, delivery); err != nil {
		t.Fatal(err)
	}
	current, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.LastJobType != model.TaskJobTypeTextSource || current.Status != model.TaskStatusQueued || current.Stage != model.TaskStageUploaded {
		t.Fatalf("handoff=%+v", current)
	}
	var identity textsource.Identity
	if json.Unmarshal([]byte(current.MediaIdentityJSON), &identity) != nil || identity.CID != 222 || identity.PartIndex != 2 || identity.MediaFingerprint != current.FileMD5 {
		t.Fatalf("media source binding=%+v", identity)
	}
	sourceJob, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTextSource)
	if sourceJob == nil || sourceJob.Stage != model.TaskStageTextSource || sourceJob.ProcessingToken == "" || sourceJob.LeaseExpiresAt == nil || sourceJob.RetryBudgetID == "" {
		t.Fatalf("source intent not recoverable=%+v", sourceJob)
	}
	if err := c.handleDownload(ctx, delivery); err != nil && !errors.Is(err, errStaleDispatch) {
		t.Fatal(err)
	}
	if downloads != 1 || adapter.probes != 1 {
		t.Fatalf("repeated transfer downloads=%d probes=%d", downloads, adapter.probes)
	}
}
