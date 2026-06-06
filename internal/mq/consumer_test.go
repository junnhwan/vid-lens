package mq

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

type recordingAI struct {
	summarizeInput  string
	chunksInput     []string
	transcribeInput []string
	transcribeUsed  bool
	transcripts     map[string]string
}

type emptyProfileResolver struct{}

func (emptyProfileResolver) GetDefaultAIProfile(int64) (*ai.Profile, error) {
	return nil, nil
}

func (a *recordingAI) Transcribe(_ context.Context, audioPath string) (string, error) {
	a.transcribeUsed = true
	a.transcribeInput = append(a.transcribeInput, audioPath)
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
