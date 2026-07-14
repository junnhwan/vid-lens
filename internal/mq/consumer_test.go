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
	"vid-lens/internal/observability"
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

type staticProfileResolver struct {
	profile *ai.Profile
	err     error
}

func (r staticProfileResolver) GetDefaultAIProfile(int64) (*ai.Profile, error) {
	return r.profile, r.err
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
		"task_id=42",
		"chunk_count=2",
		"chunk_index=1",
		"output_chars=5",
		"chunk_index=2",
		"output_chars=7",
		"output_chars=14",
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

func TestIndexAfterTranscriptionEnqueuesRAGIndexAndDoesNotCallIndexer(t *testing.T) {
	calls := 0
	task := &model.VideoTask{ID: 12, UserID: 7, TraceID: "trace-task-12"}
	producer := &recordingRAGIndexProducer{}
	consumer := &Consumer{}
	consumer.SetRAGIndexProducer(producer)
	consumer.SetRAGIndexer(func(_ context.Context, got *model.VideoTask) error {
		calls++
		return nil
	})

	consumer.indexAfterTranscription(context.Background(), task)

	if calls != 0 {
		t.Fatalf("rag index calls = %d, want async enqueue only", calls)
	}
	if len(producer.taskIDs) != 1 || producer.taskIDs[0] != task.ID {
		t.Fatalf("rag index enqueues = %#v, want task %d", producer.taskIDs, task.ID)
	}
	if len(producer.traceIDs) != 1 || producer.traceIDs[0] != task.TraceID {
		t.Fatalf("rag index trace IDs = %#v, want %q", producer.traceIDs, task.TraceID)
	}
}

func TestIndexAfterTranscriptionCreatesQueuedRAGIndexJob(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	task := &model.VideoTask{
		UserID:   7,
		FileMD5:  "adadadadadadadadadadadadadadadad",
		Filename: "video.mp4",
		Status:   model.TaskStatusRunning,
		Stage:    model.TaskStageIndexing,
		TraceID:  "trace-rag-queued",
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	producer := &recordingRAGIndexProducer{}
	consumer := &Consumer{repo: repos}
	consumer.SetRAGIndexProducer(producer)

	if err := consumer.indexAfterTranscription(context.Background(), task); err != nil {
		t.Fatalf("index after transcription: %v", err)
	}

	job, err := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeRAGIndex)
	if err != nil {
		t.Fatalf("find rag job: %v", err)
	}
	if job == nil || job.Status != model.TaskStatusQueued || job.Stage != model.TaskStageIndexing || job.TraceID != task.TraceID {
		t.Fatalf("rag task_job = %+v, want queued/indexing", job)
	}
	if job.RetryBudgetID == "" {
		t.Fatal("rag task_job retry budget ID is empty")
	}
	if len(producer.budgetIDs) != 1 || producer.budgetIDs[0] != job.RetryBudgetID {
		t.Fatalf("rag enqueue budget IDs = %#v, want %q", producer.budgetIDs, job.RetryBudgetID)
	}
	if _, err := repos.RetryBudget.Get(job.RetryBudgetID); err != nil {
		t.Fatalf("find rag retry budget: %v", err)
	}
}

func TestIndexAfterTranscriptionRecordsRAGIndexFailureWhenEnqueueFails(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	task := &model.VideoTask{
		UserID:   7,
		FileMD5:  "abababababababababababababababab",
		Filename: "video.mp4",
		Status:   model.TaskStatusRunning,
		Stage:    model.TaskStageIndexing,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	profile := &ai.Profile{EmbeddingModel: "text-embedding-3-small", EmbeddingDim: 1536}
	consumer := &Consumer{
		repo:     repos,
		profiles: staticProfileResolver{profile: profile},
	}
	consumer.SetRAGIndexProducer(&recordingRAGIndexProducer{err: fmt.Errorf("kafka unavailable")})

	consumer.indexAfterTranscription(context.Background(), task)

	index, err := repos.RAGIndex.FindByTaskAndModel(task.UserID, task.ID, profile.EmbeddingModel)
	if err != nil {
		t.Fatalf("find rag index: %v", err)
	}
	if index == nil {
		t.Fatal("expected rag index failure row")
	}
	if index.Status != model.RAGIndexStatusFailed {
		t.Fatalf("rag index status = %q, want failed", index.Status)
	}
	if !strings.Contains(index.LastError, "kafka unavailable") {
		t.Fatalf("rag index last_error = %q, want kafka error", index.LastError)
	}
	job, err := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeRAGIndex)
	if err != nil {
		t.Fatalf("find rag job: %v", err)
	}
	if job == nil || job.Status != model.TaskStatusFailed || job.LastErrorCode != "enqueue_failed" {
		t.Fatalf("rag task_job enqueue failure = %+v", job)
	}
}

func TestHandleRAGIndexInvokesIndexer(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	task := &model.VideoTask{
		UserID:   7,
		FileMD5:  "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd",
		Filename: "video.mp4",
		Status:   model.TaskStatusRunning,
		Stage:    model.TaskStageIndexing,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{
		TaskID:  task.ID,
		Content: "已完成转写，RAG consumer 应异步构建索引",
		Words:   22,
	}); err != nil {
		t.Fatalf("create transcription: %v", err)
	}

	calls := 0
	consumer := &Consumer{repo: repos}
	consumer.SetRAGIndexer(func(_ context.Context, got *model.VideoTask) error {
		calls++
		if got.ID != task.ID || got.UserID != task.UserID {
			t.Fatalf("indexed task = %+v, want %+v", got, task)
		}
		return nil
	})

	if err := consumer.handleRAGIndex(context.Background(), ragIndexMessage(task.ID, "trace-rag-ok")); err != nil {
		t.Fatalf("handleRAGIndex: %v", err)
	}

	if calls != 1 {
		t.Fatalf("rag index calls = %d, want 1", calls)
	}
	current, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if current.Status != model.TaskStatusCompleted || current.Stage != model.TaskStageNone {
		t.Fatalf("task status/stage = %d/%q, want completed/none", current.Status, current.Stage)
	}
	job, err := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeRAGIndex)
	if err != nil {
		t.Fatalf("find rag job: %v", err)
	}
	if job == nil || job.Status != model.TaskStatusCompleted || job.Stage != model.TaskStageIndexing || job.FinishedAt == nil {
		t.Fatalf("rag task_job = %+v, want completed", job)
	}
}

func TestHandleRAGIndexCarriesKafkaRetryBudgetIntoAIContext(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	task := &model.VideoTask{
		UserID: 7, FileMD5: "dededededededededededededededede",
		Filename: "budget.mp4", Status: model.TaskStatusRunning, Stage: model.TaskStageIndexing,
		MaxRetries: 3,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.TaskJob.UpsertQueued(task, model.TaskJobTypeRAGIndex, model.TaskStageIndexing, 3); err != nil {
		t.Fatalf("create task job: %v", err)
	}
	job, err := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeRAGIndex)
	if err != nil || job == nil {
		t.Fatalf("find task job: %+v %v", job, err)
	}
	now := time.Date(2026, 7, 14, 14, 0, 0, 0, time.UTC)
	if _, err := repos.RetryBudget.Ensure(repository.RetryBudgetSpec{
		BudgetID: "rag-budget-1", TaskID: task.ID, JobID: job.ID,
		Operation: model.TaskJobTypeRAGIndex, MaxAttempts: 3,
		Deadline: now.Add(time.Hour), Now: now,
	}); err != nil {
		t.Fatalf("create retry budget: %v", err)
	}
	if _, err := repos.TaskJob.BindRetryBudget(task.ID, model.TaskJobTypeRAGIndex, "rag-budget-1"); err != nil {
		t.Fatalf("bind retry budget: %v", err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "用于验证预算透传的转写", Words: 12}); err != nil {
		t.Fatalf("create transcription: %v", err)
	}

	var got ai.GovernanceContext
	consumer := &Consumer{repo: repos}
	consumer.SetRAGIndexer(func(ctx context.Context, _ *model.VideoTask) error {
		got = ai.GovernanceContextFromContext(ctx)
		return nil
	})
	payload, _ := json.Marshal(RAGIndexPayload{TaskID: task.ID, TraceID: "trace-rag-budget", BudgetID: "rag-budget-1"})
	if err := consumer.handleRAGIndex(context.Background(), kafka.Message{Value: payload}); err != nil {
		t.Fatalf("handleRAGIndex: %v", err)
	}
	if got.RetryBudgetID != "rag-budget-1" {
		t.Fatalf("governance retry budget = %q, want rag-budget-1", got.RetryBudgetID)
	}
	if got.Subject != "user:7" {
		t.Fatalf("governance subject = %q, want user:7", got.Subject)
	}
}

func TestHandleRAGIndexFailureSchedulesRetryButKeepsTranscription(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	task := &model.VideoTask{
		UserID:     7,
		FileMD5:    "efefefefefefefefefefefefefefefef",
		Filename:   "video.mp4",
		Status:     model.TaskStatusRunning,
		Stage:      model.TaskStageIndexing,
		MaxRetries: 3,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{
		TaskID:  task.ID,
		Content: "RAG 失败后转写文本仍应保留",
		Words:   16,
	}); err != nil {
		t.Fatalf("create transcription: %v", err)
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
	consumer.SetRAGIndexer(func(context.Context, *model.VideoTask) error {
		return fmt.Errorf("milvus service unavailable")
	})

	if err := consumer.handleRAGIndex(context.Background(), ragIndexMessage(task.ID, "trace-rag-fail")); err != nil {
		t.Fatalf("handleRAGIndex: %v", err)
	}

	current, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if current.Status != model.TaskStatusFailed || current.Stage != model.TaskStageIndexing {
		t.Fatalf("task status/stage = %d/%q, want failed/indexing", current.Status, current.Stage)
	}
	if current.LastJobType != TaskJobRAGIndex {
		t.Fatalf("last_job_type = %q, want rag_index", current.LastJobType)
	}
	if current.NextRetryAt == nil || !current.NextRetryAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("next_retry_at = %v, want %v", current.NextRetryAt, now.Add(time.Minute))
	}
	job, err := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeRAGIndex)
	if err != nil {
		t.Fatalf("find rag job: %v", err)
	}
	if job == nil || job.Status != model.TaskStatusFailed || job.NextRetryAt == nil || !job.NextRetryAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("rag task_job retry metadata = %+v", job)
	}

	transcription, err := repos.Transcription.FindByTaskID(task.ID)
	if err != nil {
		t.Fatalf("find transcription: %v", err)
	}
	if transcription == nil || transcription.Content == "" {
		t.Fatalf("transcription should remain readable after rag failure: %+v", transcription)
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
	if current.AssetID == nil || *current.AssetID == 0 || current.FileURL == "" || current.FileSize != int64(len(videoBytes)) {
		t.Fatalf("task asset fields not populated: %+v", current)
	}
	job, err := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeDownload)
	if err != nil {
		t.Fatalf("find download job: %v", err)
	}
	if job == nil || job.Status != model.TaskStatusCompleted || job.Stage != model.TaskStageDownloading || job.FinishedAt == nil {
		t.Fatalf("download task_job = %+v, want completed", job)
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
	if current.AssetID == nil || *current.AssetID != asset.ID || current.FileURL != asset.ObjectName || current.FileMD5 != fileMD5 {
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
	if !strings.Contains(logText, "video download failed") {
		t.Fatalf("expected structured failure event, got: %s", logText)
	}
	if strings.Contains(logText, "www.bilibili.com") || strings.Contains(logText, "token=secret") || strings.Contains(logText, "#frag") {
		t.Fatalf("log leaked source URL, query, or fragment: %s", logText)
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
	job, err := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTranscribe)
	if err != nil {
		t.Fatalf("find transcribe job: %v", err)
	}
	if job == nil || job.Status != model.TaskStatusFailed || job.RetryCount != 2 || job.NextRetryAt == nil || !job.NextRetryAt.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("transcribe task_job retry metadata = %+v", job)
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

func TestRetrySchedulerRestoresNextRetryWhenEnqueueFailsAfterClaim(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	dueAt := now.Add(-time.Second)
	task := &model.VideoTask{
		UserID:       7,
		FileMD5:      "99999999999999999999999999999999",
		Filename:     "retry-dispatch-fail.mp4",
		Status:       model.TaskStatusFailed,
		Stage:        model.TaskStageTranscribing,
		RetryCount:   2,
		MaxRetries:   3,
		NextRetryAt:  &dueAt,
		LastJobType:  TaskJobTranscribe,
		LastErrorMsg: "previous network timeout",
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	producer := &recordingRetryProducer{err: fmt.Errorf("kafka unavailable")}
	scheduler := NewRetryScheduler(repos, producer, RetrySchedulerConfig{
		BatchSize: 10,
		Now:       func() time.Time { return now },
	})
	if err := scheduler.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce() expected enqueue error")
	}

	current, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if current.Status != model.TaskStatusFailed {
		t.Fatalf("status = %d, want failed", current.Status)
	}
	if current.Stage != model.TaskStageTranscribing {
		t.Fatalf("stage = %q, want transcribing", current.Stage)
	}
	if current.NextRetryAt == nil || !current.NextRetryAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("next_retry_at = %v, want %v", current.NextRetryAt, now.Add(time.Minute))
	}
	if current.RetryCount != 2 {
		t.Fatalf("retry_count = %d, want unchanged 2", current.RetryCount)
	}
	if current.LastErrorCode != "retry_enqueue_failed" {
		t.Fatalf("last_error_code = %q, want retry_enqueue_failed", current.LastErrorCode)
	}
	if !strings.Contains(current.LastErrorMsg, "kafka unavailable") {
		t.Fatalf("last_error_msg = %q, want kafka error", current.LastErrorMsg)
	}
	job, err := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTranscribe)
	if err != nil {
		t.Fatalf("find transcribe job: %v", err)
	}
	if job == nil || job.Status != model.TaskStatusFailed || job.NextRetryAt == nil || !job.NextRetryAt.Equal(now.Add(time.Minute)) || job.LastErrorCode != "retry_enqueue_failed" {
		t.Fatalf("transcribe task_job dispatch failure = %+v", job)
	}
}

func TestRetrySchedulerConsumesAndForwardsBoundRetryBudget(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Date(2026, 7, 14, 15, 0, 0, 0, time.UTC)
	dueAt := now.Add(-time.Second)
	task := &model.VideoTask{
		UserID: 7, FileMD5: "abababababababababababababababab", Filename: "retry-budget.mp4",
		Status: model.TaskStatusFailed, Stage: model.TaskStageTranscribing,
		RetryCount: 1, MaxRetries: 3, NextRetryAt: &dueAt, LastJobType: TaskJobTranscribe,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.TaskJob.UpsertQueued(task, TaskJobTranscribe, model.TaskStageTranscribing, 3); err != nil {
		t.Fatalf("create task job: %v", err)
	}
	if err := repos.TaskJob.RecordRetryableFailure(task.ID, TaskJobTranscribe, model.TaskStageTranscribing, "provider 503", 1, 3, dueAt); err != nil {
		t.Fatalf("mark task job retryable: %v", err)
	}
	job, err := repos.TaskJob.FindByTaskAndType(task.ID, TaskJobTranscribe)
	if err != nil || job == nil {
		t.Fatalf("find task job: %+v %v", job, err)
	}
	if _, err := repos.RetryBudget.Ensure(repository.RetryBudgetSpec{
		BudgetID: "task-retry-budget-1", TaskID: task.ID, JobID: job.ID,
		Operation: TaskJobTranscribe, MaxAttempts: 3, Deadline: now.Add(time.Hour), Now: now,
	}); err != nil {
		t.Fatalf("create retry budget: %v", err)
	}
	if _, err := repos.TaskJob.BindRetryBudget(task.ID, TaskJobTranscribe, "task-retry-budget-1"); err != nil {
		t.Fatalf("bind retry budget: %v", err)
	}

	producer := &recordingRetryProducer{}
	scheduler := NewRetryScheduler(repos, producer, RetrySchedulerConfig{
		BatchSize: 10, Now: func() time.Time { return now }, NewToken: func() string { return "dispatch-token-1" },
	})
	if err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(producer.transcribeBudgetIDs) != 1 || producer.transcribeBudgetIDs[0] != "task-retry-budget-1" {
		t.Fatalf("forwarded budgets = %#v, want task-retry-budget-1", producer.transcribeBudgetIDs)
	}
	budget, err := repos.RetryBudget.Get("task-retry-budget-1")
	if err != nil {
		t.Fatalf("get retry budget: %v", err)
	}
	if budget.AttemptCount != 1 {
		t.Fatalf("scheduler consumed attempts = %d, want 1", budget.AttemptCount)
	}
}

func TestRetrySchedulerRequeuesRAGIndexJob(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	dueAt := now.Add(-time.Second)
	task := &model.VideoTask{
		UserID:      7,
		FileMD5:     "12121212121212121212121212121212",
		Filename:    "rag-retry.mp4",
		Status:      model.TaskStatusFailed,
		Stage:       model.TaskStageIndexing,
		RetryCount:  1,
		MaxRetries:  3,
		NextRetryAt: &dueAt,
		LastJobType: TaskJobRAGIndex,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	producer := &recordingRetryProducer{}
	scheduler := NewRetryScheduler(repos, producer, RetrySchedulerConfig{
		BatchSize: 10,
		Now:       func() time.Time { return now },
	})
	if err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(producer.ragIndexes) != 1 || producer.ragIndexes[0] != task.ID {
		t.Fatalf("rag index requeues = %#v, want task %d", producer.ragIndexes, task.ID)
	}
	requeued, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatalf("find requeued task: %v", err)
	}
	if requeued.Status != model.TaskStatusQueued || requeued.Stage != model.TaskStageIndexing || requeued.NextRetryAt != nil {
		t.Fatalf("requeued task = %+v, want queued/indexing with nil next_retry_at", requeued)
	}
}

type recordingRetryProducer struct {
	analyzes            []int64
	transcribes         []int64
	downloads           []int64
	ragIndexes          []int64
	transcribeBudgetIDs []string
	err                 error
}

func (p *recordingRetryProducer) EnqueueAnalyze(_ context.Context, taskID int64, _ string) error {
	p.analyzes = append(p.analyzes, taskID)
	return p.err
}

func (p *recordingRetryProducer) EnqueueTranscribe(ctx context.Context, taskID int64, _ string) error {
	p.transcribes = append(p.transcribes, taskID)
	p.transcribeBudgetIDs = append(p.transcribeBudgetIDs, retryBudgetIDFromContext(ctx))
	return p.err
}

func (p *recordingRetryProducer) EnqueueDownload(_ context.Context, taskID int64, _ string) error {
	p.downloads = append(p.downloads, taskID)
	return p.err
}

func (p *recordingRetryProducer) EnqueueRAGIndex(_ context.Context, taskID int64) error {
	p.ragIndexes = append(p.ragIndexes, taskID)
	return p.err
}

type recordingRAGIndexProducer struct {
	taskIDs   []int64
	traceIDs  []string
	budgetIDs []string
	err       error
}

func (p *recordingRAGIndexProducer) EnqueueRAGIndex(ctx context.Context, taskID int64) error {
	p.taskIDs = append(p.taskIDs, taskID)
	p.traceIDs = append(p.traceIDs, TraceIDFromContext(ctx))
	p.budgetIDs = append(p.budgetIDs, RetryBudgetIDFromContext(ctx))
	return p.err
}

func downloadMessage(taskID int64, key string) kafka.Message {
	payload, _ := json.Marshal(DownloadPayload{TaskID: taskID, Key: key})
	return kafka.Message{Key: []byte(key), Value: payload}
}

func ragIndexMessage(taskID int64, traceID string) kafka.Message {
	payload, _ := json.Marshal(RAGIndexPayload{TaskID: taskID, TraceID: traceID})
	return kafka.Message{Key: []byte(fmt.Sprint(taskID)), Value: payload}
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
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get test sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}

	return repository.NewRepositories(db)
}

func TestTranscribeAudioOverridesAnalyzeContextWithActualStage(t *testing.T) {
	strategy := &stageCapturingStrategy{}
	consumer := &Consumer{ffmpegPath: "ffmpeg", splitAudio: func(context.Context, string, string, int) ([]string, error) { return []string{"chunk-1.mp3"}, nil }}
	ctx := observability.WithCorrelation(context.Background(), observability.Correlation{Stage: model.TaskStageSummarizing, Attempt: 2})
	if _, err := consumer.transcribeAudio(ctx, 42, "audio.mp3", strategy); err != nil {
		t.Fatal(err)
	}
	if strategy.transcribe.Stage != model.TaskStageTranscribing || strategy.transcribe.Attempt != 2 {
		t.Fatalf("correlation=%+v", strategy.transcribe)
	}
}

func TestSummarizeTaskOverridesContextWithActualStage(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "92929292929292929292929292929292", Filename: "summary.mp4", Status: model.TaskStatusRunning, Stage: model.TaskStageTranscribing}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "transcript"}); err != nil {
		t.Fatal(err)
	}
	strategy := &stageCapturingStrategy{}
	consumer := &Consumer{repo: repos, ai: strategy}
	ctx := observability.WithCorrelation(context.Background(), observability.Correlation{Stage: model.TaskStageTranscribing, Attempt: 3})
	if err := consumer.summarizeTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	if strategy.summarize.Stage != model.TaskStageSummarizing || strategy.summarize.Attempt != 3 {
		t.Fatalf("correlation=%+v", strategy.summarize)
	}
}

type stageCapturingStrategy struct{ transcribe, summarize observability.Correlation }

func (s *stageCapturingStrategy) Transcribe(ctx context.Context, _ string) (string, error) {
	s.transcribe = observability.CorrelationFromContext(ctx)
	return "text", nil
}
func (s *stageCapturingStrategy) TranscribeChunks(ctx context.Context, _ []string) (string, error) {
	s.transcribe = observability.CorrelationFromContext(ctx)
	return "text", nil
}
func (s *stageCapturingStrategy) Summarize(ctx context.Context, _ string) (string, error) {
	s.summarize = observability.CorrelationFromContext(ctx)
	return "summary", nil
}
