package service

import (
	"context"
	"testing"
	"time"

	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

func TestGetVisualProgressHidesUnknownTotalAndPreviousAttempt(t *testing.T) {
	repos := newMediaTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Filename: "lesson.mp4", Status: model.TaskStatusQueued}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	svc := &MediaService{repo: repos}
	if _, err := svc.GetVisualProgress(context.Background(), 8, task.ID); err == nil {
		t.Fatal("other user read visual progress")
	}
	now := time.Now()
	claim, err := repos.ClaimTaskProcessing(repository.TaskProcessingClaimRequest{
		TaskID: task.ID, JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing,
		Now: now, LeaseUntil: now.Add(time.Hour), NewToken: "visual-worker-1",
	})
	if err != nil || claim.Outcome != repository.TaskLeaseAcquired {
		t.Fatalf("claim: %+v, %v", claim, err)
	}
	lease := repository.TaskProcessingLeaseRequest{TaskID: task.ID, JobType: model.TaskJobTypeTranscribe, Token: claim.Token, Now: now}
	if ok, err := repos.BeginVisualProgress(lease); err != nil || !ok {
		t.Fatalf("begin: %v, %v", ok, err)
	}
	got, err := svc.GetVisualProgress(context.Background(), 7, task.ID)
	if err != nil || got.TotalFrames != nil || got.Status != model.VisualProgressQueued || got.AttemptID == "" || got.AttemptID == claim.Token {
		t.Fatalf("unknown total leaked or attempt missing: %+v, %v", got, err)
	}
	if ok, err := repos.AdvanceVisualProgress(lease, repository.VisualProgressUpdate{
		Status: model.VisualProgressRunning, Phase: "observing_frames", TotalKnown: true,
		TotalFrames: 3, Processed: 1, VisionFailed: 1,
	}); err != nil || !ok {
		t.Fatalf("advance: %v, %v", ok, err)
	}
	got, err = svc.GetVisualProgress(context.Background(), 7, task.ID)
	if err != nil || got.TotalFrames == nil || *got.TotalFrames != 3 || got.ProcessedFrames != 1 || got.VisionFailedFrames != 1 {
		t.Fatalf("stored progress unavailable after refresh: %+v, %v", got, err)
	}
	// A replacement worker must not show counters from the previous attempt.
	replacement, err := repos.ClaimTaskProcessing(repository.TaskProcessingClaimRequest{
		TaskID: task.ID, JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing,
		Now: now.Add(2 * time.Hour), LeaseUntil: now.Add(3 * time.Hour), NewToken: "visual-worker-2",
	})
	if err != nil || replacement.Outcome != repository.TaskLeaseAcquired {
		t.Fatalf("replacement: %+v, %v", replacement, err)
	}
	got, err = svc.GetVisualProgress(context.Background(), 7, task.ID)
	if err != nil || got.Status != "waiting_to_start" || got.TotalFrames != nil || got.ProcessedFrames != 0 || got.AttemptID != "" {
		t.Fatalf("new dispatch inherited old attempt: %+v, %v", got, err)
	}
}
