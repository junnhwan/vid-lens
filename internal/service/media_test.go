package service

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
	"vid-lens/internal/mq"
	"vid-lens/internal/repository"
)

func TestRemoteVideoURLValidatorRejectsUnsafeTargets(t *testing.T) {
	validator := remoteVideoURLValidator{
		allowedHosts: []string{"bilibili.com", "youtube.com", "youtu.be"},
		resolver: fakeRemoteURLResolver{
			"internal.bilibili.com": {net.ParseIP("10.0.0.8")},
			"www.bilibili.com":      {net.ParseIP("203.0.113.10")},
		},
	}
	cases := []string{
		"http://localhost/video.mp4",
		"http://127.0.0.1/video.mp4",
		"http://[::1]/video.mp4",
		"file:///tmp/video.mp4",
		"https://evilbilibili.com/video/BV1xx411c7mD",
		"https://internal.bilibili.com/video/BV1xx411c7mD",
	}

	for _, rawURL := range cases {
		if _, err := validator.validate(context.Background(), rawURL); err == nil {
			t.Fatalf("expected %q to be rejected", rawURL)
		}
	}
}

func TestRemoteVideoURLValidatorAllowsWhitelistedPublicHostsAndSanitizes(t *testing.T) {
	validator := remoteVideoURLValidator{
		allowedHosts: []string{"bilibili.com", "youtube.com", "youtu.be"},
		resolver: fakeRemoteURLResolver{
			"www.bilibili.com": {net.ParseIP("203.0.113.10")},
		},
	}

	checked, err := validator.validate(context.Background(), "https://www.bilibili.com/video/BV1xx411c7mD?p=1&token=secret#frag")
	if err != nil {
		t.Fatalf("expected URL to be allowed, got %v", err)
	}
	if checked.Sanitized != "https://www.bilibili.com/video/BV1xx411c7mD" {
		t.Fatalf("sanitized URL = %q", checked.Sanitized)
	}
}

func TestRemoteVideoURLValidatorKeepsYouTubeVideoIDWhileSanitizing(t *testing.T) {
	validator := remoteVideoURLValidator{
		allowedHosts: []string{"youtube.com", "youtu.be"},
		resolver: fakeRemoteURLResolver{
			"www.youtube.com": {net.ParseIP("203.0.113.10")},
		},
	}

	checked, err := validator.validate(context.Background(), "https://www.youtube.com/watch?v=abc123&list=secret#frag")
	if err != nil {
		t.Fatalf("expected YouTube URL to be allowed, got %v", err)
	}
	if checked.Sanitized != "https://www.youtube.com/watch?v=abc123" {
		t.Fatalf("sanitized YouTube URL = %q", checked.Sanitized)
	}
}

func TestUploadByURLCreatesDownloadingTaskAndEnqueuesDownload(t *testing.T) {
	repos := newMediaTestRepositories(t)
	producer := &recordingMediaProducer{}

	svc := &MediaService{
		repo: repos,
		mq:   producer,
		tools: config.ToolsConfig{
			YtDlpPath:         filepathThatDoesNotExist(),
			FFmpegPath:        "ffmpeg",
			AllowedVideoHosts: []string{"bilibili.com"},
		},
		remoteURLResolver: fakeRemoteURLResolver{
			"www.bilibili.com": {net.ParseIP("203.0.113.10")},
		},
	}

	rawURL := "https://www.bilibili.com/video/BV1xx411c7mD?p=1&token=secret#frag"
	sanitizedURL := "https://www.bilibili.com/video/BV1xx411c7mD"
	result, err := svc.UploadByURL(context.Background(), 7, rawURL)
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
	if result.FileMD5 != md5HexString(sanitizedURL) {
		t.Fatalf("file md5 = %q, want sanitized URL md5", result.FileMD5)
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
	if task.SourceURL != sanitizedURL {
		t.Fatalf("task source_url = %q, want sanitized URL %q", task.SourceURL, sanitizedURL)
	}
	job, err := repos.TaskJob.FindByTaskAndType(result.TaskID, model.TaskJobTypeDownload)
	if err != nil {
		t.Fatalf("find download job: %v", err)
	}
	if job == nil {
		t.Fatal("expected download task_job")
	}
	if job.Status != model.TaskStatusQueued || job.Stage != model.TaskStageDownloading || job.UserID != 7 || job.TraceID != result.TraceID {
		t.Fatalf("download task_job = %+v", job)
	}
}

func TestUploadByURLCreatesTaskWithoutAssetBeforeDownloadWhenForeignKeysAreEnforced(t *testing.T) {
	repos := newMediaTestRepositoriesWithForeignKeys(t)
	producer := &recordingMediaProducer{}

	svc := &MediaService{
		repo: repos,
		mq:   producer,
		tools: config.ToolsConfig{
			AllowedVideoHosts: []string{"youtube.com"},
		},
		remoteURLResolver: fakeRemoteURLResolver{
			"www.youtube.com": {net.ParseIP("203.0.113.10")},
		},
	}

	result, err := svc.UploadByURL(context.Background(), 7, "https://www.youtube.com/watch?v=abc123")
	if err != nil {
		t.Fatalf("UploadByURL() error = %v", err)
	}
	task, err := repos.Task.FindByID(result.TaskID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if task.AssetID != nil {
		t.Fatalf("URL task should not reference an asset before download completes, got %v", *task.AssetID)
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
	job, err := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTranscribe)
	if err != nil {
		t.Fatalf("find transcribe job: %v", err)
	}
	if job == nil || job.Status != model.TaskStatusQueued || job.Stage != model.TaskStageTranscribing || job.TraceID != "trace-transcribe" {
		t.Fatalf("transcribe task_job = %+v", job)
	}
	if job.RetryBudgetID == "" || len(producer.transcribeBudgetIDs) != 1 || producer.transcribeBudgetIDs[0] != job.RetryBudgetID {
		t.Fatalf("transcribe retry budget job/context = %q/%#v", job.RetryBudgetID, producer.transcribeBudgetIDs)
	}
	if _, err := repos.RetryBudget.Get(job.RetryBudgetID); err != nil {
		t.Fatalf("load transcribe retry budget: %v", err)
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
	job, err := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeAnalyze)
	if err != nil {
		t.Fatalf("find analyze job: %v", err)
	}
	if job == nil || job.Status != model.TaskStatusQueued || job.Stage != model.TaskStageSummarizing || job.TraceID != "trace-analyze" {
		t.Fatalf("analyze task_job = %+v", job)
	}
	if job.RetryBudgetID == "" || len(producer.analyzeBudgetIDs) != 1 || producer.analyzeBudgetIDs[0] != job.RetryBudgetID {
		t.Fatalf("analyze retry budget job/context = %q/%#v", job.RetryBudgetID, producer.analyzeBudgetIDs)
	}
	if _, err := repos.RetryBudget.Get(job.RetryBudgetID); err != nil {
		t.Fatalf("load analyze retry budget: %v", err)
	}
}

func TestDeleteTaskKeepsSharedAssetObjectAndCleansTaskData(t *testing.T) {
	repos := newMediaTestRepositories(t)
	storage := &recordingObjectStorage{}
	cleaner := &recordingTaskVectorCleaner{}

	asset := createMediaTestAsset(t, repos, "cccccccccccccccccccccccccccccccc", "videos/shared-delete.mp4")
	taskA := createMediaTestTask(t, repos, 7, asset, "a.mp4")
	taskB := createMediaTestTask(t, repos, 8, asset, "b.mp4")
	createTaskOwnedData(t, repos, taskA.ID, taskA.UserID, "text-embedding-3-small")
	if err := repos.TaskJob.UpsertQueued(taskA, model.TaskJobTypeTranscribe, model.TaskStageTranscribing, 3); err != nil {
		t.Fatalf("create task job: %v", err)
	}

	svc := &MediaService{
		repo:              repos,
		objectDeleter:     storage,
		taskVectorCleaner: cleaner,
	}
	if err := svc.DeleteTask(context.Background(), taskA.UserID, taskA.ID); err != nil {
		t.Fatalf("DeleteTask() error = %v", err)
	}

	if _, err := repos.Task.FindByID(taskA.ID); err == nil {
		t.Fatal("deleted task should not be visible")
	}
	if _, err := repos.Task.FindByID(taskB.ID); err != nil {
		t.Fatalf("shared task should remain visible: %v", err)
	}
	if len(storage.deleted) != 0 {
		t.Fatalf("shared object should not be deleted, got %v", storage.deleted)
	}
	foundAsset, err := repos.Asset.FindByMD5(asset.FileMD5)
	if err != nil {
		t.Fatalf("find asset: %v", err)
	}
	if foundAsset == nil {
		t.Fatal("shared asset should remain")
	}
	assertTaskOwnedDataDeleted(t, repos, taskA.ID, taskA.UserID, "text-embedding-3-small")
	jobs, err := repos.TaskJob.ListByTaskID(taskA.UserID, taskA.ID)
	if err != nil {
		t.Fatalf("list task jobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("task jobs should be deleted, got %+v", jobs)
	}
	if len(cleaner.calls) != 1 || cleaner.calls[0].userID != taskA.UserID || cleaner.calls[0].taskID != taskA.ID || cleaner.calls[0].model != "text-embedding-3-small" {
		t.Fatalf("unexpected vector cleanup calls: %+v", cleaner.calls)
	}
}

func TestDeleteTaskDeletesLastAssetReferenceAndObject(t *testing.T) {
	repos := newMediaTestRepositories(t)
	storage := &recordingObjectStorage{}
	cleaner := &recordingTaskVectorCleaner{}

	asset := createMediaTestAsset(t, repos, "dddddddddddddddddddddddddddddddd", "videos/only-delete.mp4")
	task := createMediaTestTask(t, repos, 7, asset, "only.mp4")
	createTaskOwnedData(t, repos, task.ID, task.UserID, "text-embedding-3-small")

	svc := &MediaService{
		repo:              repos,
		objectDeleter:     storage,
		taskVectorCleaner: cleaner,
	}
	if err := svc.DeleteTask(context.Background(), task.UserID, task.ID); err != nil {
		t.Fatalf("DeleteTask() error = %v", err)
	}

	if len(storage.deleted) != 1 || storage.deleted[0] != asset.ObjectName {
		t.Fatalf("deleted objects = %v, want [%s]", storage.deleted, asset.ObjectName)
	}
	foundAsset, err := repos.Asset.FindByMD5(asset.FileMD5)
	if err != nil {
		t.Fatalf("find asset: %v", err)
	}
	if foundAsset != nil {
		t.Fatalf("asset should be deleted after last reference, got %+v", foundAsset)
	}
	assertTaskOwnedDataDeleted(t, repos, task.ID, task.UserID, "text-embedding-3-small")
}

func TestDeleteTaskClearsCompletedUploadStateForLastAssetReference(t *testing.T) {
	repos := newMediaTestRepositories(t)
	storage := &recordingObjectStorage{}
	redisServer := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	asset := createMediaTestAsset(t, repos, "abababababababababababababababab", "videos/delete-upload-state.mp4")
	task := createMediaTestTask(t, repos, 7, asset, "delete-upload-state.mp4")
	key := "upload:chunks:" + asset.FileMD5
	ctx := context.Background()
	for _, suffix := range []string{"", ":status", ":total", ":file_size", ":chunk_size"} {
		if err := rdb.Set(ctx, key+suffix, "stale", 0).Err(); err != nil {
			t.Fatalf("seed redis key %s: %v", suffix, err)
		}
	}

	svc := &MediaService{repo: repos, objectDeleter: storage, rdb: rdb}
	if err := svc.DeleteTask(ctx, task.UserID, task.ID); err != nil {
		t.Fatalf("DeleteTask() error = %v", err)
	}

	for _, suffix := range []string{"", ":status", ":total", ":file_size", ":chunk_size"} {
		if rdb.Exists(ctx, key+suffix).Val() != 0 {
			t.Errorf("stale upload state remains: %s", key+suffix)
		}
	}
}

func TestCheckUploadProgressRejectsStaleCompletedStateWithoutActiveAsset(t *testing.T) {
	repos := newMediaTestRepositories(t)
	redisServer := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	ctx := context.Background()
	md5 := "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd"
	key := "upload:chunks:" + md5
	rdb.Set(ctx, key+":status", "COMPLETED", 0)
	rdb.Set(ctx, key+":file_size", 11, 0)
	rdb.Set(ctx, key+":chunk_size", 5, 0)
	rdb.Set(ctx, key+":total", 3, 0)

	svc := &MediaService{repo: repos, rdb: rdb, cfg: config.UploadConfig{ChunkSize: 5}}
	result, err := svc.CheckUploadProgress(ctx, md5, 11, 5, 3)
	if err != nil {
		t.Fatalf("CheckUploadProgress() error = %v", err)
	}
	if result["status"] == "completed" {
		t.Fatalf("stale completed state must not skip upload: %#v", result)
	}
}

func TestDeleteTaskStopsWhenVectorCleanupFails(t *testing.T) {
	repos := newMediaTestRepositories(t)
	storage := &recordingObjectStorage{}
	cleaner := &recordingTaskVectorCleaner{err: errors.New("milvus delete failed")}

	asset := createMediaTestAsset(t, repos, "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", "videos/vector-fail.mp4")
	task := createMediaTestTask(t, repos, 7, asset, "vector-fail.mp4")
	createTaskOwnedData(t, repos, task.ID, task.UserID, "text-embedding-3-small")

	svc := &MediaService{
		repo:              repos,
		objectDeleter:     storage,
		taskVectorCleaner: cleaner,
	}
	if err := svc.DeleteTask(context.Background(), task.UserID, task.ID); err == nil {
		t.Fatal("DeleteTask() expected vector cleanup error")
	}

	if _, err := repos.Task.FindByID(task.ID); err != nil {
		t.Fatalf("task should remain when vector cleanup fails: %v", err)
	}
	if len(storage.deleted) != 0 {
		t.Fatalf("object should not be deleted when vector cleanup fails, got %v", storage.deleted)
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
	analyzes            []int64
	analyzeTraceIDs     []string
	analyzeBudgetIDs    []string
	transcribes         []int64
	transcribeTraceIDs  []string
	transcribeBudgetIDs []string
}

type recordingObjectStorage struct {
	deleted []string
}

func (s *recordingObjectStorage) DeleteObject(_ context.Context, objectName string) error {
	s.deleted = append(s.deleted, objectName)
	return nil
}

type recordingTaskVectorCleaner struct {
	calls []struct {
		userID int64
		taskID int64
		model  string
	}
	err error
}

type fakeRemoteURLResolver map[string][]net.IP

func (r fakeRemoteURLResolver) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	_ = ctx
	ips := r[strings.ToLower(host)]
	if ips == nil {
		return []net.IP{net.ParseIP("203.0.113.20")}, nil
	}
	return ips, nil
}

func (c *recordingTaskVectorCleaner) DeleteTaskChunks(_ context.Context, userID, taskID int64, embeddingModel string) error {
	c.calls = append(c.calls, struct {
		userID int64
		taskID int64
		model  string
	}{userID: userID, taskID: taskID, model: embeddingModel})
	return c.err
}

func createMediaTestAsset(t *testing.T, repos *repository.Repositories, md5, objectName string) *model.VideoAsset {
	t.Helper()
	asset := &model.VideoAsset{
		FileMD5:     md5,
		ObjectName:  objectName,
		FileSize:    1024,
		ContentType: "video/mp4",
	}
	if err := repos.Asset.Create(asset); err != nil {
		t.Fatalf("create asset: %v", err)
	}
	return asset
}

func createMediaTestTask(t *testing.T, repos *repository.Repositories, userID int64, asset *model.VideoAsset, filename string) *model.VideoTask {
	t.Helper()
	task := &model.VideoTask{
		UserID:   userID,
		AssetID:  &asset.ID,
		FileMD5:  asset.FileMD5,
		Filename: filename,
		FileURL:  asset.ObjectName,
		FileSize: asset.FileSize,
		Status:   model.TaskStatusPending,
		Stage:    model.TaskStageUploaded,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	return task
}

func createTaskOwnedData(t *testing.T, repos *repository.Repositories, taskID, userID int64, embeddingModel string) {
	t.Helper()
	if err := repos.Transcription.Create(&model.VideoTranscription{TaskID: taskID, Content: "transcript", Words: 1}); err != nil {
		t.Fatalf("create transcription: %v", err)
	}
	if err := repos.TranscriptionChunk.UpsertCompleted(taskID, 0, "audio/chunk-0.mp3", "chunk transcript"); err != nil {
		t.Fatalf("create transcription chunk: %v", err)
	}
	if err := repos.Summary.Create(&model.AISummary{TaskID: taskID, Content: "summary", ModelName: "llm"}); err != nil {
		t.Fatalf("create summary: %v", err)
	}
	if err := repos.VideoChunk.ReplaceTaskChunks(taskID, embeddingModel, []model.VideoChunk{
		{
			UserID:         userID,
			TaskID:         taskID,
			ChunkIndex:     0,
			Content:        "rag chunk",
			ContentHash:    "hash",
			EmbeddingModel: embeddingModel,
			EmbeddingDim:   1536,
			VectorID:       "vector-id",
		},
	}); err != nil {
		t.Fatalf("create video chunk: %v", err)
	}
	if err := repos.RAGIndex.Upsert(&model.VideoRAGIndex{
		UserID:         userID,
		TaskID:         taskID,
		EmbeddingModel: embeddingModel,
		EmbeddingDim:   1536,
		Status:         model.RAGIndexStatusIndexed,
		ChunkCount:     1,
	}); err != nil {
		t.Fatalf("create rag index: %v", err)
	}
	session := &model.ChatSession{UserID: userID, TaskID: taskID, Title: "chat"}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatalf("create chat session: %v", err)
	}
	if err := repos.Chat.CreateMessage(&model.ChatMessage{SessionID: session.ID, UserID: userID, Role: "user", Content: "question"}); err != nil {
		t.Fatalf("create chat message: %v", err)
	}
}

func assertTaskOwnedDataDeleted(t *testing.T, repos *repository.Repositories, taskID, userID int64, embeddingModel string) {
	t.Helper()
	transcription, err := repos.Transcription.FindByTaskID(taskID)
	if err != nil {
		t.Fatalf("find transcription: %v", err)
	}
	if transcription != nil {
		t.Fatalf("transcription should be deleted, got %+v", transcription)
	}
	transcriptionChunks, err := repos.TranscriptionChunk.ListByTaskID(taskID)
	if err != nil {
		t.Fatalf("list transcription chunks: %v", err)
	}
	if len(transcriptionChunks) != 0 {
		t.Fatalf("transcription chunks should be deleted, got %+v", transcriptionChunks)
	}
	summary, err := repos.Summary.FindByTaskID(taskID)
	if err != nil {
		t.Fatalf("find summary: %v", err)
	}
	if summary != nil {
		t.Fatalf("summary should be deleted, got %+v", summary)
	}
	videoChunks, err := repos.VideoChunk.ListByTaskID(userID, taskID, embeddingModel)
	if err != nil {
		t.Fatalf("list video chunks: %v", err)
	}
	if len(videoChunks) != 0 {
		t.Fatalf("video chunks should be deleted, got %+v", videoChunks)
	}
	ragIndex, err := repos.RAGIndex.FindByTaskAndModel(userID, taskID, embeddingModel)
	if err != nil {
		t.Fatalf("find rag index: %v", err)
	}
	if ragIndex != nil {
		t.Fatalf("rag index should be deleted, got %+v", ragIndex)
	}
	sessions, err := repos.Chat.ListSessions(userID, taskID)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("chat sessions should be deleted, got %+v", sessions)
	}
}

func (p *recordingMediaProducer) EnqueueAnalyze(ctx context.Context, taskID int64, _ string) error {
	p.analyzes = append(p.analyzes, taskID)
	p.analyzeTraceIDs = append(p.analyzeTraceIDs, mq.TraceIDFromContext(ctx))
	p.analyzeBudgetIDs = append(p.analyzeBudgetIDs, mq.RetryBudgetIDFromContext(ctx))
	return nil
}

func (p *recordingMediaProducer) EnqueueTranscribe(ctx context.Context, taskID int64, _ string) error {
	p.transcribes = append(p.transcribes, taskID)
	p.transcribeTraceIDs = append(p.transcribeTraceIDs, mq.TraceIDFromContext(ctx))
	p.transcribeBudgetIDs = append(p.transcribeBudgetIDs, mq.RetryBudgetIDFromContext(ctx))
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

func newMediaTestRepositoriesWithForeignKeys(t *testing.T) *repository.Repositories {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	return repository.NewRepositories(db)
}

func TestUploadSpecMatchesRejectsLegacyOrChangedChunkLayout(t *testing.T) {
	if uploadSpecMatches([]interface{}{nil, nil, nil}, 117, 5, 24) {
		t.Fatal("legacy upload state without metadata must not be resumed")
	}
	if uploadSpecMatches([]interface{}{"117", "1", "117"}, 117, 5, 24) {
		t.Fatal("a 1 MiB layout must not be resumed as a 5 MiB layout")
	}
	if !uploadSpecMatches([]interface{}{"117", "5", "24"}, 117, 5, 24) {
		t.Fatal("matching upload specification should be resumable")
	}
}
