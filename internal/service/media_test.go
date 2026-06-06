package service

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
	"vid-lens/internal/mq"
	"vid-lens/internal/repository"
)

func TestValidateRemoteVideoURLRejectsLocalTargets(t *testing.T) {
	cases := []string{
		"http://localhost/video.mp4",
		"http://127.0.0.1/video.mp4",
		"http://[::1]/video.mp4",
		"file:///tmp/video.mp4",
	}

	for _, rawURL := range cases {
		if err := validateRemoteVideoURL(rawURL); err == nil {
			t.Fatalf("expected %q to be rejected", rawURL)
		}
	}
}

func TestValidateRemoteVideoURLAllowsHTTPVideoSites(t *testing.T) {
	cases := []string{
		"https://www.bilibili.com/video/BV1xx411c7mD",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
	}

	for _, rawURL := range cases {
		if err := validateRemoteVideoURL(rawURL); err != nil {
			t.Fatalf("expected %q to be allowed, got %v", rawURL, err)
		}
	}
}

func TestUploadByURLCreatesDownloadingTaskAndEnqueuesDownload(t *testing.T) {
	repos := newMediaTestRepositories(t)
	producer := &recordingMediaProducer{}

	svc := &MediaService{
		repo: repos,
		mq:   producer,
		tools: config.ToolsConfig{
			YtDlpPath:  filepathThatDoesNotExist(),
			FFmpegPath: "ffmpeg",
		},
	}

	result, err := svc.UploadByURL(context.Background(), 7, "https://www.bilibili.com/video/BV1xx411c7mD?p=1&token=secret#frag")
	if err != nil {
		t.Fatalf("UploadByURL() error = %v", err)
	}
	if result.TaskID == 0 {
		t.Fatal("expected task id")
	}
	if result.Status != model.TaskStatusRunning {
		t.Fatalf("result status = %d, want running", result.Status)
	}
	if result.Stage != model.TaskStageDownloading {
		t.Fatalf("result stage = %q, want downloading", result.Stage)
	}
	if result.FileMD5 == "" {
		t.Fatal("expected deterministic placeholder md5 before download finishes")
	}
	if result.TraceID == "" {
		t.Fatal("expected trace id")
	}

	if len(producer.downloads) != 1 {
		t.Fatalf("download enqueue calls = %d, want 1", len(producer.downloads))
	}
	if producer.downloads[0].taskID != result.TaskID || producer.downloads[0].key != result.FileMD5 {
		t.Fatalf("unexpected download enqueue: %+v result=%+v", producer.downloads[0], result)
	}
	if producer.downloads[0].traceID != result.TraceID {
		t.Fatalf("download trace_id = %q, want %q", producer.downloads[0].traceID, result.TraceID)
	}

	task, err := repos.Task.FindByID(result.TaskID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if task.UserID != 7 {
		t.Fatalf("task user = %d, want 7", task.UserID)
	}
	if task.Status != model.TaskStatusRunning || task.Stage != model.TaskStageDownloading {
		t.Fatalf("task status/stage = %d/%q, want running/downloading", task.Status, task.Stage)
	}
	if task.TraceID != result.TraceID {
		t.Fatalf("task trace_id = %q, want %q", task.TraceID, result.TraceID)
	}
	if task.SourceType != model.TaskSourceTypeURL {
		t.Fatalf("task source_type = %q, want url", task.SourceType)
	}
	if task.SourceURL == "" || !strings.Contains(task.SourceURL, "token=secret") {
		t.Fatalf("task source_url was not persisted for worker: %q", task.SourceURL)
	}
}

func TestRequestTranscribeQueuesTaskWithTranscribingStage(t *testing.T) {
	repos := newMediaTestRepositories(t)
	producer := &recordingMediaProducer{}
	task := &model.VideoTask{
		UserID:   7,
		FileMD5:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Filename: "video.mp4",
		FileURL:  "videos/video.mp4",
		Status:   model.TaskStatusPending,
		Stage:    model.TaskStageUploaded,
		TraceID:  "trace-transcribe",
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	svc := &MediaService{repo: repos, mq: producer}
	if err := svc.RequestTranscribe(context.Background(), 7, task.ID); err != nil {
		t.Fatalf("RequestTranscribe: %v", err)
	}

	current, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if current.Status != model.TaskStatusQueued || current.Stage != model.TaskStageTranscribing {
		t.Fatalf("status/stage = %d/%q, want queued/transcribing", current.Status, current.Stage)
	}
	if len(producer.transcribes) != 1 || producer.transcribes[0] != task.ID {
		t.Fatalf("transcribe enqueue calls = %#v, want task id", producer.transcribes)
	}
	if len(producer.transcribeTraceIDs) != 1 || producer.transcribeTraceIDs[0] != "trace-transcribe" {
		t.Fatalf("transcribe trace ids = %#v, want trace-transcribe", producer.transcribeTraceIDs)
	}
}

func TestRequestAnalysisQueuesTaskWithSummarizingStage(t *testing.T) {
	repos := newMediaTestRepositories(t)
	producer := &recordingMediaProducer{}
	task := &model.VideoTask{
		UserID:   7,
		FileMD5:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Filename: "video.mp4",
		FileURL:  "videos/video.mp4",
		Status:   model.TaskStatusPending,
		Stage:    model.TaskStageUploaded,
		TraceID:  "trace-analyze",
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	svc := &MediaService{repo: repos, mq: producer}
	if err := svc.RequestAnalysis(context.Background(), 7, task.ID); err != nil {
		t.Fatalf("RequestAnalysis: %v", err)
	}

	current, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if current.Status != model.TaskStatusQueued || current.Stage != model.TaskStageSummarizing {
		t.Fatalf("status/stage = %d/%q, want queued/summarizing", current.Status, current.Stage)
	}
	if len(producer.analyzes) != 1 || producer.analyzes[0] != task.ID {
		t.Fatalf("analyze enqueue calls = %#v, want task id", producer.analyzes)
	}
	if len(producer.analyzeTraceIDs) != 1 || producer.analyzeTraceIDs[0] != "trace-analyze" {
		t.Fatalf("analyze trace ids = %#v, want trace-analyze", producer.analyzeTraceIDs)
	}
}

func filepathThatDoesNotExist() string {
	return os.DevNull + "-vidlens-missing-ytdlp"
}

type recordingMediaProducer struct {
	downloads []struct {
		taskID  int64
		key     string
		traceID string
	}
	analyzes           []int64
	analyzeTraceIDs    []string
	transcribes        []int64
	transcribeTraceIDs []string
}

func (p *recordingMediaProducer) EnqueueAnalyze(ctx context.Context, taskID int64, _ string) error {
	p.analyzes = append(p.analyzes, taskID)
	p.analyzeTraceIDs = append(p.analyzeTraceIDs, mq.TraceIDFromContext(ctx))
	return nil
}

func (p *recordingMediaProducer) EnqueueTranscribe(ctx context.Context, taskID int64, _ string) error {
	p.transcribes = append(p.transcribes, taskID)
	p.transcribeTraceIDs = append(p.transcribeTraceIDs, mq.TraceIDFromContext(ctx))
	return nil
}

func (p *recordingMediaProducer) EnqueueDownload(ctx context.Context, taskID int64, key string) error {
	p.downloads = append(p.downloads, struct {
		taskID  int64
		key     string
		traceID string
	}{taskID: taskID, key: key, traceID: mq.TraceIDFromContext(ctx)})
	return nil
}

func newMediaTestRepositories(t *testing.T) *repository.Repositories {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	return repository.NewRepositories(db)
}
