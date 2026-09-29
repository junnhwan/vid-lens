package repository

import (
	"testing"

	"vid-lens/internal/model"
)

func TestTaskListFiltersIncludeIndependentSummaryJob(t *testing.T) {
	repos := newTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "abababababababababababababababab", Filename: "lesson.mp4", Status: model.TaskStatusCompleted}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.Transcription.Create(&model.VideoTranscription{TaskID: task.ID, FileMD5: task.FileMD5, Content: "saved transcript"}); err != nil {
		t.Fatal(err)
	}
	job := &model.TaskJob{TaskID: task.ID, UserID: 7, JobType: model.TaskJobTypeSummary, Status: model.TaskStatusQueued, Stage: model.TaskStageSummarizing}
	if err := repos.db.Create(job).Error; err != nil {
		t.Fatal(err)
	}
	assertFilter := func(filter string, want int64) {
		t.Helper()
		rows, total, err := repos.Task.ListByUserID(7, 1, 10, "", filter)
		if err != nil || total != want || int64(len(rows)) != want {
			t.Fatalf("filter=%s total=%d rows=%d err=%v, want %d", filter, total, len(rows), err, want)
		}
	}
	assertFilter("processing", 1)
	assertFilter("failed", 0)
	if err := repos.db.Model(job).Updates(map[string]interface{}{"status": model.TaskStatusFailed, "last_error_code": "non_retryable_error"}).Error; err != nil {
		t.Fatal(err)
	}
	assertFilter("processing", 0)
	assertFilter("failed", 1)
	if rows, total, err := repos.Task.ListByUserID(8, 1, 10, "", "failed"); err != nil || total != 0 || len(rows) != 0 {
		t.Fatalf("other user rows=%d total=%d err=%v", len(rows), total, err)
	}
}
