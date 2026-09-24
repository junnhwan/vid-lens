package service

import (
	"context"
	"testing"

	"vid-lens/internal/model"
)

func TestSetTaskVisualDisabledPreservesExistingResults(t *testing.T) {
	repos := newMediaTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", Filename: "video.mp4", Status: model.TaskStatusCompleted}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.Transcription.Create(&model.VideoTranscription{TaskID: task.ID, FileMD5: task.FileMD5, Content: "转写正文"}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Summary.Create(&model.AISummary{TaskID: task.ID, FileMD5: task.FileMD5, Content: "摘要正文"}); err != nil {
		t.Fatal(err)
	}
	if err := repos.VisualFrame.ReplaceTaskFrames(task.ID, []model.VideoVisualFrame{{TaskID: task.ID, FrameIndex: 0, TimeMs: 1000, OCRText: "课件", Status: model.VisualFrameStatusCompleted}}); err != nil {
		t.Fatal(err)
	}
	svc := &MediaService{repo: repos}
	updated, err := svc.SetTaskVisualDisabled(context.Background(), 7, task.ID, true)
	if err != nil || !updated.VisualDisabled {
		t.Fatalf("setting update = %+v, %v", updated, err)
	}
	transcription, _ := repos.Transcription.FindByTaskID(task.ID)
	summary, _ := repos.Summary.FindByTaskID(task.ID)
	frames, _ := repos.VisualFrame.ListByTaskID(task.ID)
	if transcription == nil || transcription.Content != "转写正文" || summary == nil || summary.Content != "摘要正文" || len(frames) != 1 || frames[0].OCRText != "课件" {
		t.Fatalf("existing results changed: transcription=%+v summary=%+v frames=%+v", transcription, summary, frames)
	}
}
