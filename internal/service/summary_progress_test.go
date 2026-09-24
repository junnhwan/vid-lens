package service

import (
	"context"
	"testing"

	"vid-lens/internal/model"
)

func TestTaskDetailShowsIncompleteSummaryCoverageWithoutPublishingReport(t *testing.T) {
	repos := newMediaTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "34343434343434343434343434343434", Filename: "long.mp4", Status: model.TaskStatusFailed, Stage: model.TaskStageSummarizing}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	for i, status := range []string{"completed", "failed", "pending"} {
		if err := repos.SummaryPart.Upsert(&model.SummaryPart{TaskID: task.ID, Level: 0, PartIndex: i, InputHash: "hash", Status: status, Content: "intermediate text", StartMS: int64(i) * 300000, EndMS: int64(i+1) * 300000}); err != nil {
			t.Fatal(err)
		}
	}
	svc := &MediaService{repo: repos}
	detail, err := svc.GetTaskDetail(context.Background(), 7, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.HasSummary || detail.Summary != nil {
		t.Fatal("partial summary was exposed as a finished report")
	}
	progress := detail.SummaryProgress
	if progress == nil || progress.Phase != "segments" || progress.Completed != 1 || progress.Total != 3 || progress.FailedPart != 2 || progress.Current != 2 {
		t.Fatalf("unexpected progress: %+v", progress)
	}
}
