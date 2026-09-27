package mq

import (
	"context"
	"errors"
	"testing"
	"time"

	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

func TestVisualBranchCancellationWhileWaitingForSlotPersistsOutcome(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "cccccccccccccccccccccccccccccccc", Filename: "lesson.mp4", Status: model.TaskStatusQueued}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	claim, err := repos.ClaimTaskProcessing(repository.TaskProcessingClaimRequest{
		TaskID: task.ID, JobType: TaskJobTranscribe, Stage: model.TaskStageTranscribing,
		Now: now, LeaseUntil: now.Add(time.Hour), NewToken: "canceled-visual-worker",
	})
	if err != nil || claim.Outcome != repository.TaskLeaseAcquired {
		t.Fatalf("claim: %+v, %v", claim, err)
	}
	called := make(chan struct{}, 1)
	c := &Consumer{repo: repos, now: func() time.Time { return now }, visualIndex: func(context.Context, *model.VideoTask) (int, error) {
		called <- struct{}{}
		return 0, nil
	}}
	c.SetVisualConcurrency(1)
	c.visualSlots <- struct{}{}
	base := withProcessingLeaseOwner(context.Background(), &processingLeaseOwner{
		repos: repos, taskID: task.ID, jobType: TaskJobTranscribe, token: claim.Token, now: c.currentTime,
	})
	ctx, cancel := context.WithCancel(base)
	wait := c.startVisualIndexBranch(ctx, task)
	cancel()
	out := wait()
	if !errors.Is(out.err, context.Canceled) {
		t.Fatalf("branch outcome: %+v", out)
	}
	select {
	case <-called:
		t.Fatal("visual index ran without a slot")
	default:
	}
	row, err := repos.VisualProgress.Find(task.ID)
	if err != nil || row == nil || row.Status != model.VisualProgressCanceled || row.Phase != "waiting_for_slot" {
		t.Fatalf("canceled progress: %+v, %v", row, err)
	}
}
