package repository

import (
	"fmt"
	"testing"
	"time"
	"vid-lens/internal/model"
)

func TestTaskListFiltersBeforeCountingAndStablePagination(t *testing.T) {
	repos := newTestRepositories(t)
	instant := time.Now()
	for i := 0; i < 105; i++ {
		task := &model.VideoTask{UserID: 7, FileMD5: fmt.Sprintf("video-%d", i), Title: fmt.Sprintf("Video %03d", i), Filename: "lesson.mp4", CreatedAt: instant, Status: model.TaskStatusCompleted}
		if i%2 == 0 {
			task.Status = model.TaskStatusRunning
		}
		if err := repos.Task.Create(task); err != nil {
			t.Fatal(err)
		}
	}
	if err := repos.Task.Create(&model.VideoTask{UserID: 8, FileMD5: "other", Title: "Video other", Filename: "other.mp4", Status: model.TaskStatusRunning}); err != nil {
		t.Fatal(err)
	}
	seen := map[int64]bool{}
	for page := 1; page <= 3; page++ {
		list, total, err := repos.Task.ListByUserID(7, page, 24, "video", "processing")
		if err != nil || total != 53 {
			t.Fatalf("total %d err %v", total, err)
		}
		for _, task := range list {
			if task.UserID != 7 || task.Status != model.TaskStatusRunning || seen[task.ID] {
				t.Fatalf("unstable or unscoped page: %+v", task)
			}
			seen[task.ID] = true
		}
	}
	if len(seen) != 53 {
		t.Fatal(len(seen))
	}
	list, total, err := repos.Task.ListByUserID(7, 1, 24, "Video 104", "processing")
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("search %+v %d %v", list, total, err)
	}
}

func TestTaskListReadySupportsVisualAndDoesNotShareEmptyFingerprint(t *testing.T) {
	repos := newTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "", Filename: "empty.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.Transcription.Create(&model.VideoTranscription{TaskID: 999, FileMD5: "", Content: "other content"}); err != nil {
		t.Fatal(err)
	}
	_, total, err := repos.Task.ListByUserID(7, 1, 20, "", "ready")
	if err != nil || total != 0 {
		t.Fatalf("empty md5 shared content: %d %v", total, err)
	}
	if err := repos.VisualFrame.ReplaceTaskFrames(task.ID, []model.VideoVisualFrame{{TaskID: task.ID, Status: model.VisualFrameStatusCompleted, OCRText: "saved slide"}}); err != nil {
		t.Fatal(err)
	}
	_, total, err = repos.Task.ListByUserID(7, 1, 20, "", "ready")
	if err != nil || total != 1 {
		t.Fatalf("visual content: %d %v", total, err)
	}
}
