package mq

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

type recordingAI struct {
	summarizeInput string
}

func (a *recordingAI) Transcribe(context.Context, string) (string, error) {
	return "", nil
}

func (a *recordingAI) TranscribeChunks(context.Context, []string) (string, error) {
	return "", nil
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
