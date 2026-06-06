package mq

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/segmentio/kafka-go"
	"gorm.io/gorm"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

type recordingAI struct {
	summarizeInput   string
	chunksInput      []string
	transcribeInput  []string
	transcribeUsed   bool
	transcripts      map[string]string
	transcribeErrors map[string]error
}

type emptyProfileResolver struct{}

func (emptyProfileResolver) GetDefaultAIProfile(int64) (*ai.Profile, error) {
	return nil, nil
}

func (a *recordingAI) Transcribe(_ context.Context, audioPath string) (string, error) {
	a.transcribeUsed = true
	a.transcribeInput = append(a.transcribeInput, audioPath)
	if a.transcribeErrors != nil && a.transcribeErrors[audioPath] != nil {
		return "", a.transcribeErrors[audioPath]
	}
	if a.transcripts != nil {
		return a.transcripts[audioPath], nil
	}
	return "分片转写结果", nil
}

func (a *recordingAI) TranscribeChunks(_ context.Context, audioPaths []string) (string, error) {
	a.chunksInput = append([]string(nil), audioPaths...)
	return "分片转写结果", nil
}

func (a *recordingAI) Summarize(_ context.Context, text string) (string, error) {
	a.summarizeInput = text
	return "总结来自已有转写", nil
}

func TestSummarizeTaskReusesExistingTranscription(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	task := &model.VideoTask{
		UserID:   1,
		FileMD5:  "cccccccccccccccccccccccccccccccc",
		Filename: "video.mp4",
		FileURL:  "videos/video.mp4",
		Status:   model.TaskStatusCompleted,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{
		TaskID:  task.ID,
		Content: "已有转写文本",
		Words:   6,
	}); err != nil {
		t.Fatalf("create transcription: %v", err)
	}

	ai := &recordingAI{}
	consumer := &Consumer{repo: repos, ai: ai}
	if err := consumer.summarizeTask(context.Background(), task); err != nil {
		t.Fatalf("summarize task: %v", err)
	}

	if ai.summarizeInput != "已有转写文本" {
		t.Fatalf("expected existing transcription to be summarized, got %q", ai.summarizeInput)
	}
	summary, err := repos.Summary.FindByTaskID(task.ID)
	if err != nil {
		t.Fatalf("find summary: %v", err)
	}
	if summary == nil || summary.Content != "总结来自已有转写" {
		t.Fatalf("unexpected saved summary: %#v", summary)
	}
}

func TestProcessVideoReusesExistingTranscriptionBeforeDownloadingVideo(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	task := &model.VideoTask{
		UserID:   1,
		FileMD5:  "dddddddddddddddddddddddddddddddd",
		Filename: "video.mp4",
		FileURL:  "videos/video.mp4",
		Status:   model.TaskStatusCompleted,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{
		TaskID:  task.ID,
		Content: "已存在转写，不应重新下载视频",
		Words:   15,
	}); err != nil {
		t.Fatalf("create transcription: %v", err)
	}

	ai := &recordingAI{}
	consumer := &Consumer{repo: repos, ai: ai}
	if err := consumer.processVideo(context.Background(), task); err != nil {
		t.Fatalf("process video: %v", err)
	}

	if ai.summarizeInput != "已存在转写，不应重新下载视频" {
		t.Fatalf("expected existing transcription to be summarized, got %q", ai.summarizeInput)
	}
}

func TestStrategyForTaskDoesNotFallbackWhenProfileResolverIsConfigured(t *testing.T) {
	globalAI := &recordingAI{}
	consumer := &Consumer{
		ai:        globalAI,
		aiFactory: ai.NewFactory(),
		profiles:  emptyProfileResolver{},
	}

	_, err := consumer.strategyForTask(&model.VideoTask{UserID: 99})
	if err == nil {
		t.Fatal("strategyForTask() succeeded without user profile, want error")
	}
	if !strings.Contains(err.Error(), "请先配置 AI 服务") {
		t.Fatalf("strategyForTask() error = %v", err)
	}
	if globalAI.transcribeUsed || globalAI.summarizeInput != "" {
		t.Fatal("global AI fallback was used")
	}
}

func TestTranscribeAudioAlwaysSplitsAudioBeforeASR(t *testing.T) {
	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "audio.mp3")
	if err := os.WriteFile(audioPath, []byte("small audio"), 0644); err != nil {
		t.Fatalf("write audio: %v", err)
	}

	ai := &recordingAI{}
	consumer := &Consumer{
		ai:         ai,
		ffmpegPath: "ffmpeg",
		splitAudio: func(context.Context, string, string, int) ([]string, error) {
			return []string{"chunk-001.mp3", "chunk-002.mp3"}, nil
		},
	}

	transcript, err := consumer.transcribeAudio(context.Background(), 0, audioPath, ai)
	if err != nil {
		t.Fatalf("transcribe audio: %v", err)
	}
	if transcript != "分片转写结果\n\n分片转写结果" {
		t.Fatalf("unexpected transcript: %q", transcript)
	}
	if !ai.transcribeUsed {
		t.Fatalf("expected ASR to be called for generated chunks")
	}
	if len(ai.transcribeInput) != 2 {
		t.Fatalf("expected 2 ASR chunk calls, got %#v", ai.transcribeInput)
	}
	if ai.transcribeInput[0] != "chunk-001.mp3" || ai.transcribeInput[1] != "chunk-002.mp3" {
		t.Fatalf("unexpected ASR inputs: %#v", ai.transcribeInput)
	}
}

func TestTranscribeAudioLogsChunkMetrics(t *testing.T) {
	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "audio.mp3")
	if err := os.WriteFile(audioPath, []byte("small audio"), 0644); err != nil {
		t.Fatalf("write audio: %v", err)
	}

	chunkA := filepath.Join(tmpDir, "chunks", "chunk-001.mp3")
	chunkB := filepath.Join(tmpDir, "chunks", "chunk-002.mp3")
	if err := os.MkdirAll(filepath.Dir(chunkA), 0755); err != nil {
		t.Fatalf("create chunk dir: %v", err)
	}
	if err := os.WriteFile(chunkA, []byte("chunk a"), 0644); err != nil {
		t.Fatalf("write chunk a: %v", err)
	}
	if err := os.WriteFile(chunkB, []byte("chunk b"), 0644); err != nil {
		t.Fatalf("write chunk b: %v", err)
	}

	ai := &recordingAI{
		transcripts: map[string]string{
			chunkA: "第一段文本",
			chunkB: "第二段更长文本",
		},
	}
	consumer := &Consumer{
		ai:         ai,
		ffmpegPath: "ffmpeg",
		splitAudio: func(context.Context, string, string, int) ([]string, error) {
			return []string{chunkA, chunkB}, nil
		},
	}

	var logs bytes.Buffer
	originalOutput := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(originalOutput)

	transcript, err := consumer.transcribeAudio(context.Background(), 42, audioPath, ai)
	if err != nil {
		t.Fatalf("transcribe audio: %v", err)
	}
	if transcript != "第一段文本\n\n第二段更长文本" {
		t.Fatalf("unexpected transcript: %q", transcript)
	}

	logText := logs.String()
	for _, want := range []string{
		"taskID=42",
		"chunks=2",
		"chunk=1/2",
		fmt.Sprintf("path=%s", chunkA),
		"chars=5",
		"chunk=2/2",
		fmt.Sprintf("path=%s", chunkB),
		"chars=7",
		"transcriptChars=14",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("expected log to contain %q, got:\n%s", want, logText)
		}
	}
}

func TestTranscribeAudioReturnsErrorWhenSplitCreatesNoChunks(t *testing.T) {
	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "audio.mp3")
	if err := os.WriteFile(audioPath, []byte("small audio"), 0644); err != nil {
		t.Fatalf("write audio: %v", err)
	}

	ai := &recordingAI{}
	consumer := &Consumer{
		ai:         ai,
		ffmpegPath: "ffmpeg",
		splitAudio: func(context.Context, string, string, int) ([]string, error) {
			return nil, nil
		},
	}

	_, err := consumer.transcribeAudio(context.Background(), 42, audioPath, ai)
	if err == nil {
		t.Fatalf("expected error when split creates no chunks")
	}
	if !strings.Contains(err.Error(), "没有可转写的音频片段") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTranscribeAudioPersistsChunksAndSkipsCompletedOnRetry(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "audio.mp3")
	chunkA := filepath.Join(tmpDir, "chunks", "chunk-001.mp3")
	chunkB := filepath.Join(tmpDir, "chunks", "chunk-002.mp3")
	if err := os.MkdirAll(filepath.Dir(chunkA), 0755); err != nil {
		t.Fatalf("create chunk dir: %v", err)
	}
	for _, path := range []string{audioPath, chunkA, chunkB} {
		if err := os.WriteFile(path, []byte("audio"), 0644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	if err := repos.TranscriptionChunk.UpsertCompleted(42, 0, chunkA, "已完成第一段"); err != nil {
		t.Fatalf("seed completed chunk: %v", err)
	}

	ai := &recordingAI{transcripts: map[string]string{chunkB: "第二段新文本"}}
	consumer := &Consumer{
		repo:       repos,
		ai:         ai,
		ffmpegPath: "ffmpeg",
		splitAudio: func(context.Context, string, string, int) ([]string, error) {
			return []string{chunkA, chunkB}, nil
		},
	}

	transcript, err := consumer.transcribeAudio(context.Background(), 42, audioPath, ai)
	if err != nil {
		t.Fatalf("transcribeAudio: %v", err)
	}
	if transcript != "已完成第一段\n\n第二段新文本" {
		t.Fatalf("transcript = %q", transcript)
	}
	if len(ai.transcribeInput) != 1 || ai.transcribeInput[0] != chunkB {
		t.Fatalf("ASR inputs = %#v, want only second chunk", ai.transcribeInput)
	}

	chunks, err := repos.TranscriptionChunk.ListByTaskID(42)
	if err != nil {
		t.Fatalf("list chunks: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("stored chunks = %d, want 2", len(chunks))
	}
	if chunks[0].Status != model.TranscriptionChunkStatusCompleted || chunks[0].Content != "已完成第一段" {
		t.Fatalf("chunk 0 = %+v", chunks[0])
	}
	if chunks[1].Status != model.TranscriptionChunkStatusCompleted || chunks[1].Content != "第二段新文本" || chunks[1].Chars != 6 {
		t.Fatalf("chunk 1 = %+v", chunks[1])
	}
}

func TestTranscribeAudioPersistsFailedChunk(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "audio.mp3")
	chunkA := filepath.Join(tmpDir, "chunks", "chunk-001.mp3")
	if err := os.MkdirAll(filepath.Dir(chunkA), 0755); err != nil {
		t.Fatalf("create chunk dir: %v", err)
	}
	for _, path := range []string{audioPath, chunkA} {
		if err := os.WriteFile(path, []byte("audio"), 0644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	ai := &recordingAI{transcribeErrors: map[string]error{chunkA: fmt.Errorf("asr timeout")}}
	consumer := &Consumer{
		repo:       repos,
		ai:         ai,
		ffmpegPath: "ffmpeg",
		splitAudio: func(context.Context, string, string, int) ([]string, error) {
			return []string{chunkA}, nil
		},
	}

	_, err := consumer.transcribeAudio(context.Background(), 43, audioPath, ai)
	if err == nil {
		t.Fatal("transcribeAudio succeeded, want ASR error")
	}

	chunk, findErr := repos.TranscriptionChunk.FindByTaskAndIndex(43, 0)
	if findErr != nil {
		t.Fatalf("find chunk: %v", findErr)
	}
	if chunk == nil {
		t.Fatal("expected failed chunk row")
	}
	if chunk.Status != model.TranscriptionChunkStatusFailed || chunk.ErrorMsg == "" || chunk.RetryCount != 1 {
		t.Fatalf("failed chunk = %+v", chunk)
	}
}

func TestIndexAfterTranscriptionInvokesRAGIndexerAndSwallowsError(t *testing.T) {
	calls := 0
	task := &model.VideoTask{ID: 12, UserID: 7}
	consumer := &Consumer{}
	consumer.SetRAGIndexer(func(_ context.Context, got *model.VideoTask) error {
		calls++
		if got.ID != task.ID || got.UserID != task.UserID {
			t.Fatalf("indexed task = %+v, want %+v", got, task)
		}
		return fmt.Errorf("milvus unavailable")
	})

	consumer.indexAfterTranscription(context.Background(), task)

	if calls != 1 {
		t.Fatalf("rag index calls = %d, want 1", calls)
	}
}

func TestHandleDownloadCreatesAssetAndMarksTaskUploaded(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	task := &model.VideoTask{
		UserID:     7,
		FileMD5:    "11111111111111111111111111111111",
		Filename:   "WEB_pending.mp4",
		Status:     model.TaskStatusRunning,
		Stage:      model.TaskStageDownloading,
		SourceType: model.TaskSourceTypeURL,
		SourceURL:  "https://www.youtube.com/watch?v=test",
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	tmpDir := t.TempDir()
	videoPath := filepath.Join(tmpDir, "downloaded.mp4")
	videoBytes := []byte("downloaded video content")
	if err := os.WriteFile(videoPath, videoBytes, 0644); err != nil {
		t.Fatalf("write video: %v", err)
	}
	expectedMD5 := md5Hex(videoBytes)

	uploaded := false
	consumer := &Consumer{
		repo: repos,
		downloadVideo: func(context.Context, string) (string, error) {
			return videoPath, nil
		},
		uploadLocalFile: func(_ context.Context, localPath, objectName, contentType string) error {
			uploaded = true
			if localPath != videoPath {
				t.Fatalf("uploaded path = %q, want %q", localPath, videoPath)
			}
			if !strings.HasPrefix(objectName, "videos/") || !strings.HasSuffix(objectName, ".mp4") {
				t.Fatalf("unexpected object name: %q", objectName)
			}
			if contentType != "video/mp4" {
				t.Fatalf("content type = %q, want video/mp4", contentType)
			}
			return nil
		},
	}

	if err := consumer.handleDownload(context.Background(), downloadMessage(task.ID, task.FileMD5)); err != nil {
		t.Fatalf("handleDownload: %v", err)
	}
	if !uploaded {
		t.Fatal("expected downloaded file to be uploaded when asset does not exist")
	}

	current, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if current.Status != model.TaskStatusPending || current.Stage != model.TaskStageUploaded {
		t.Fatalf("task status/stage = %d/%q, want pending/uploaded", current.Status, current.Stage)
	}
	if current.FileMD5 != expectedMD5 {
		t.Fatalf("task file md5 = %q, want %q", current.FileMD5, expectedMD5)
	}
	if current.AssetID == 0 || current.FileURL == "" || current.FileSize != int64(len(videoBytes)) {
		t.Fatalf("task asset fields not populated: %+v", current)
	}

	asset, err := repos.Asset.FindByMD5(expectedMD5)
	if err != nil {
		t.Fatalf("find asset: %v", err)
	}
	if asset == nil {
		t.Fatal("expected asset to be created")
	}
}

func TestHandleDownloadReusesExistingAssetForSameMD5(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	videoBytes := []byte("same video content")
	fileMD5 := md5Hex(videoBytes)
	asset := &model.VideoAsset{
		FileMD5:     fileMD5,
		ObjectName:  "videos/existing.mp4",
		FileSize:    int64(len(videoBytes)),
		ContentType: "video/mp4",
	}
	if err := repos.Asset.Create(asset); err != nil {
		t.Fatalf("create asset: %v", err)
	}
	task := &model.VideoTask{
		UserID:     7,
		FileMD5:    "22222222222222222222222222222222",
		Filename:   "WEB_pending.mp4",
		Status:     model.TaskStatusRunning,
		Stage:      model.TaskStageDownloading,
		SourceType: model.TaskSourceTypeURL,
		SourceURL:  "https://www.youtube.com/watch?v=test",
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	tmpDir := t.TempDir()
	videoPath := filepath.Join(tmpDir, "downloaded.mp4")
	if err := os.WriteFile(videoPath, videoBytes, 0644); err != nil {
		t.Fatalf("write video: %v", err)
	}

	consumer := &Consumer{
		repo: repos,
		downloadVideo: func(context.Context, string) (string, error) {
			return videoPath, nil
		},
		uploadLocalFile: func(context.Context, string, string, string) error {
			t.Fatal("did not expect upload when matching asset already exists")
			return nil
		},
	}

	if err := consumer.handleDownload(context.Background(), downloadMessage(task.ID, task.FileMD5)); err != nil {
		t.Fatalf("handleDownload: %v", err)
	}

	current, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if current.AssetID != asset.ID || current.FileURL != asset.ObjectName || current.FileMD5 != fileMD5 {
		t.Fatalf("task did not reuse existing asset: %+v asset=%+v", current, asset)
	}
}

func TestHandleDownloadFailureMarksTaskFailedWithoutLeakingQueryInLog(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	task := &model.VideoTask{
		UserID:     7,
		FileMD5:    "33333333333333333333333333333333",
		Filename:   "WEB_pending.mp4",
		Status:     model.TaskStatusRunning,
		Stage:      model.TaskStageDownloading,
		SourceType: model.TaskSourceTypeURL,
		SourceURL:  "https://www.bilibili.com/video/BV1xx?p=1&token=secret#frag",
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	var logs bytes.Buffer
	originalOutput := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(originalOutput)

	consumer := &Consumer{
		repo: repos,
		downloadVideo: func(context.Context, string) (string, error) {
			return "", fmt.Errorf("yt-dlp 下载失败: HTTP Error 412")
		},
	}

	if err := consumer.handleDownload(context.Background(), downloadMessage(task.ID, task.FileMD5)); err != nil {
		t.Fatalf("handleDownload: %v", err)
	}

	current, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if current.Status != model.TaskStatusFailed || current.Stage != model.TaskStageDownloading {
		t.Fatalf("task status/stage = %d/%q, want failed/downloading", current.Status, current.Stage)
	}
	if !strings.Contains(current.ErrorMsg, "yt-dlp 下载失败") {
		t.Fatalf("task error_msg = %q", current.ErrorMsg)
	}

	logText := logs.String()
	if !strings.Contains(logText, "url=https://www.bilibili.com/video/BV1xx") {
		t.Fatalf("expected sanitized URL in log, got: %s", logText)
	}
	if strings.Contains(logText, "token=secret") || strings.Contains(logText, "#frag") {
		t.Fatalf("log leaked query or fragment: %s", logText)
	}
}

func TestRecordTaskFailureSchedulesRetryForRetryableError(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	task := &model.VideoTask{
		UserID:     7,
		FileMD5:    "44444444444444444444444444444444",
		Filename:   "video.mp4",
		Status:     model.TaskStatusRunning,
		Stage:      model.TaskStageTranscribing,
		RetryCount: 1,
		MaxRetries: 3,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	consumer := &Consumer{
		repo: repos,
		retryPolicy: TaskRetryPolicy{
			MaxRetries:     3,
			BackoffSeconds: []int{60, 300, 900},
			Now:            func() time.Time { return now },
		},
	}

	if err := consumer.recordTaskFailure(task.ID, TaskJobTranscribe, model.TaskStageTranscribing, fmt.Errorf("network timeout")); err != nil {
		t.Fatalf("recordTaskFailure: %v", err)
	}

	current, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if current.Status != model.TaskStatusFailed {
		t.Fatalf("status = %d, want failed", current.Status)
	}
	if current.RetryCount != 2 {
		t.Fatalf("retry_count = %d, want 2", current.RetryCount)
	}
	if current.NextRetryAt == nil || !current.NextRetryAt.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("next_retry_at = %v, want %v", current.NextRetryAt, now.Add(5*time.Minute))
	}
	if current.LastJobType != TaskJobTranscribe {
		t.Fatalf("last_job_type = %q, want transcribe", current.LastJobType)
	}
}

func TestRecordTaskFailureMarksNonRetryableErrorFailedWithoutRetry(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	task := &model.VideoTask{
		UserID:     7,
		FileMD5:    "55555555555555555555555555555555",
		Filename:   "video.mp4",
		Status:     model.TaskStatusRunning,
		Stage:      model.TaskStageSummarizing,
		RetryCount: 0,
		MaxRetries: 3,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	consumer := &Consumer{repo: repos}
	if err := consumer.recordTaskFailure(task.ID, TaskJobAnalyze, model.TaskStageSummarizing, fmt.Errorf("请先配置 AI 服务")); err != nil {
		t.Fatalf("recordTaskFailure: %v", err)
	}

	current, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if current.Status != model.TaskStatusFailed {
		t.Fatalf("status = %d, want failed", current.Status)
	}
	if current.RetryCount != 0 {
		t.Fatalf("retry_count = %d, want 0", current.RetryCount)
	}
	if current.NextRetryAt != nil {
		t.Fatalf("next_retry_at = %v, want nil", current.NextRetryAt)
	}
}

func TestRecordTaskFailureMarksDeadWhenRetryLimitExceeded(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	task := &model.VideoTask{
		UserID:     7,
		FileMD5:    "66666666666666666666666666666666",
		Filename:   "video.mp4",
		Status:     model.TaskStatusRunning,
		Stage:      model.TaskStageDownloading,
		RetryCount: 3,
		MaxRetries: 3,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	consumer := &Consumer{repo: repos}
	if err := consumer.recordTaskFailure(task.ID, TaskJobDownload, model.TaskStageDownloading, fmt.Errorf("network timeout")); err != nil {
		t.Fatalf("recordTaskFailure: %v", err)
	}

	current, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if current.Status != model.TaskStatusDead {
		t.Fatalf("status = %d, want dead", current.Status)
	}
	if current.RetryCount != 4 {
		t.Fatalf("retry_count = %d, want 4", current.RetryCount)
	}
	if current.NextRetryAt != nil {
		t.Fatalf("next_retry_at = %v, want nil", current.NextRetryAt)
	}
}

func TestRetrySchedulerRequeuesOnlyDueFailedTasks(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	dueAt := now.Add(-time.Second)
	futureAt := now.Add(time.Hour)
	dueTask := &model.VideoTask{
		UserID:      7,
		FileMD5:     "77777777777777777777777777777777",
		Filename:    "due.mp4",
		Status:      model.TaskStatusFailed,
		Stage:       model.TaskStageTranscribing,
		RetryCount:  1,
		MaxRetries:  3,
		NextRetryAt: &dueAt,
		LastJobType: TaskJobTranscribe,
	}
	futureTask := &model.VideoTask{
		UserID:      7,
		FileMD5:     "88888888888888888888888888888888",
		Filename:    "future.mp4",
		Status:      model.TaskStatusFailed,
		Stage:       model.TaskStageDownloading,
		RetryCount:  1,
		MaxRetries:  3,
		NextRetryAt: &futureAt,
		LastJobType: TaskJobDownload,
	}
	if err := repos.Task.Create(dueTask); err != nil {
		t.Fatalf("create due task: %v", err)
	}
	if err := repos.Task.Create(futureTask); err != nil {
		t.Fatalf("create future task: %v", err)
	}

	producer := &recordingRetryProducer{}
	scheduler := NewRetryScheduler(repos, producer, RetrySchedulerConfig{
		BatchSize: 10,
		Now:       func() time.Time { return now },
	})
	if err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(producer.transcribes) != 1 || producer.transcribes[0] != dueTask.ID {
		t.Fatalf("transcribe requeues = %#v, want due task", producer.transcribes)
	}
	if len(producer.downloads) != 0 || len(producer.analyzes) != 0 {
		t.Fatalf("unexpected requeues: downloads=%#v analyzes=%#v", producer.downloads, producer.analyzes)
	}

	requeued, err := repos.Task.FindByID(dueTask.ID)
	if err != nil {
		t.Fatalf("find requeued task: %v", err)
	}
	if requeued.Status != model.TaskStatusQueued || requeued.NextRetryAt != nil {
		t.Fatalf("requeued task status/next = %d/%v, want queued/nil", requeued.Status, requeued.NextRetryAt)
	}

	unchanged, err := repos.Task.FindByID(futureTask.ID)
	if err != nil {
		t.Fatalf("find future task: %v", err)
	}
	if unchanged.Status != model.TaskStatusFailed || unchanged.NextRetryAt == nil {
		t.Fatalf("future task changed unexpectedly: %+v", unchanged)
	}
}

type recordingRetryProducer struct {
	analyzes    []int64
	transcribes []int64
	downloads   []int64
}

func (p *recordingRetryProducer) EnqueueAnalyze(_ context.Context, taskID int64, _ string) error {
	p.analyzes = append(p.analyzes, taskID)
	return nil
}

func (p *recordingRetryProducer) EnqueueTranscribe(_ context.Context, taskID int64, _ string) error {
	p.transcribes = append(p.transcribes, taskID)
	return nil
}

func (p *recordingRetryProducer) EnqueueDownload(_ context.Context, taskID int64, _ string) error {
	p.downloads = append(p.downloads, taskID)
	return nil
}

func downloadMessage(taskID int64, key string) kafka.Message {
	payload, _ := json.Marshal(DownloadPayload{TaskID: taskID, Key: key})
	return kafka.Message{Key: []byte(key), Value: payload}
}

func md5Hex(data []byte) string {
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:])
}

func newConsumerTestRepositories(t *testing.T) *repository.Repositories {
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
