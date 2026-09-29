package service

import (
	"context"
	"testing"
	"time"

	"vid-lens/internal/model"
)

func TestSummaryCanStartWhileVisualOrIndexWorkContinues(t *testing.T) {
	for _, stage := range []string{model.TaskStageVisual, model.TaskStageIndexing} {
		t.Run(stage, func(t *testing.T) {
			repos := newMediaTestRepositories(t)
			until := time.Now().Add(time.Hour)
			task := &model.VideoTask{UserID: 7, FileMD5: "summary-parallel-test", Filename: "lesson.mp4",
				Status: model.TaskStatusRunning, Stage: stage, LastJobType: model.TaskJobTypeTranscribe,
				ProcessingToken: "video-worker", LeaseKind: model.TaskLeaseKindProcessing, LeaseExpiresAt: &until}
			if err := repos.Task.Create(task); err != nil {
				t.Fatal(err)
			}
			if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, FileMD5: task.FileMD5, Content: "已完成的完整转写"}); err != nil {
				t.Fatal(err)
			}
			producer := &recordingMediaProducer{}
			svc := &MediaService{repo: repos, mq: producer}
			if err := svc.RequestAnalysis(context.Background(), task.UserID, task.ID, false); err != nil {
				t.Fatalf("summary blocked by %s: %v", stage, err)
			}
			current, err := repos.Task.FindByID(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if current.Status != task.Status || current.Stage != stage || current.ProcessingToken != "video-worker" {
				t.Fatalf("summary overwrote video work: %+v", current)
			}
			job, err := repos.TaskJob.FindByTaskAndType(task.ID, "summary")
			if err != nil || job == nil || job.Status != model.TaskStatusQueued || job.ProcessingToken == "video-worker" {
				t.Fatalf("independent summary job: %+v / %v", job, err)
			}
			if err := svc.RequestAnalysis(context.Background(), task.UserID, task.ID, true); err == nil {
				t.Fatal("duplicate summary accepted")
			}
			if len(producer.analyzes) != 1 {
				t.Fatalf("duplicate model work queued: %v", producer.analyzes)
			}
		})
	}
}
