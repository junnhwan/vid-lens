package service

import (
	"strings"
	"testing"

	"vid-lens/internal/model"
)

func TestVideoQuestionsRequireOwnedCompletedContentAndRefreshOnChange(t *testing.T) {
	repos := newMediaTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "99999999999999999999999999999999", Filename: "lesson.mp4", FileURL: "video/lesson.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	svc := &MediaService{repo: repos}
	if _, err := svc.VideoQuestions(8, task.ID); err == nil {
		t.Fatal("other user could read questions")
	}
	waiting, err := svc.VideoQuestions(7, task.ID)
	if err != nil || waiting.Status != "waiting_transcription" || len(waiting.Questions) != 0 {
		t.Fatalf("waiting = %+v, %v", waiting, err)
	}
	transcript := &model.VideoTranscription{TaskID: task.ID, FileMD5: task.FileMD5, Content: "本课程介绍向量检索如何帮助定位视频中的相关片段。"}
	if err := repos.Transcription.Create(transcript); err != nil {
		t.Fatal(err)
	}
	first, err := svc.VideoQuestions(7, task.ID)
	if err != nil || first.Status != "ready" || len(first.Questions) == 0 {
		t.Fatalf("first = %+v, %v", first, err)
	}
	if !strings.Contains(first.Questions[0].Question, "向量检索") {
		t.Fatalf("not grounded: %+v", first.Questions)
	}
	again, err := svc.VideoQuestions(7, task.ID)
	if err != nil || again.ContentVersion != first.ContentVersion {
		t.Fatalf("cache mismatch: %+v, %v", again, err)
	}
	transcript.Content = "本课程介绍画面文字如何帮助定位视频中的相关片段。"
	if err := repos.Transcription.Upsert(transcript); err != nil {
		t.Fatal(err)
	}
	updated, err := svc.VideoQuestions(7, task.ID)
	if err != nil || updated.ContentVersion == first.ContentVersion || !strings.Contains(updated.Questions[0].Question, "画面文字") {
		t.Fatalf("stale recommendation: %+v, %v", updated, err)
	}
}
