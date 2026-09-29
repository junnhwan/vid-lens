package mq

import (
	"context"
	"errors"
	"testing"

	"vid-lens/internal/model"
)

func TestCompletedASRReportsVisualStageWhileWaiting(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		name := "enabled"
		if disabled {
			name = "disabled"
		}
		t.Run(name, func(t *testing.T) {
			repos, consumer, task, token, _ := newTranscribeCompletionFixture(t)
			task.VisualDisabled = disabled
			consumer.visualIndex = func(context.Context, *model.VideoTask) (int, error) { return 25, nil }
			ctx := withProcessingLeaseOwner(context.Background(), &processingLeaseOwner{
				repos: repos, taskID: task.ID, jobType: TaskJobTranscribe, token: token, now: consumer.currentTime,
			})
			waited := false
			err := consumer.waitForVisualAfterASR(ctx, task, func() visualIndexOutcome {
				waited = true
				current, err := repos.Task.FindByID(task.ID)
				if err != nil {
					t.Fatal(err)
				}
				wantStage := model.TaskStageVisual
				if disabled {
					wantStage = model.TaskStageTranscribing
				}
				if current.Status != model.TaskStatusRunning || current.Stage != wantStage || current.ProcessingToken != token {
					t.Fatalf("while waiting: status=%d stage=%s, want running/%s with same lease", current.Status, current.Stage, wantStage)
				}
				transcription, err := repos.Transcription.FindByTaskID(task.ID)
				if err != nil || transcription == nil || transcription.Content == "" {
					t.Fatalf("completed transcript unavailable: %v", err)
				}
				// Visual failure remains best-effort and must not discard ASR.
				return visualIndexOutcome{err: errors.New("visual provider unavailable")}
			})
			if err != nil || !waited {
				t.Fatalf("wait result: %v; waited=%v", err, waited)
			}
		})
	}
}

func TestASRVisualStageDoesNotUpdateAfterLeaseLoss(t *testing.T) {
	repos, consumer, task, _, _ := newTranscribeCompletionFixture(t)
	consumer.visualIndex = func(context.Context, *model.VideoTask) (int, error) { return 0, nil }
	ctx := withProcessingLeaseOwner(context.Background(), &processingLeaseOwner{
		repos: repos, taskID: task.ID, jobType: TaskJobTranscribe, token: "stale-worker", now: consumer.currentTime,
	})
	err := consumer.waitForVisualAfterASR(ctx, task, func() visualIndexOutcome {
		t.Fatal("stale worker advanced to visual wait")
		return visualIndexOutcome{}
	})
	if !errors.Is(err, ErrProcessingLeaseLost) {
		t.Fatalf("got %v, want lease lost", err)
	}
	current, err := repos.Task.FindByID(task.ID)
	if err != nil || current.Stage != model.TaskStageTranscribing {
		t.Fatalf("stale worker changed stage: %+v, %v", current, err)
	}
}
