package mq

import (
	"context"
	"testing"
	"time"

	"vid-lens/internal/model"
	"vid-lens/internal/pkg/visualprogress"
	"vid-lens/internal/repository"
)

func TestVisualRetryAfterEmbeddingFailureReusesPublishedFrames(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "visual-index-retry", Filename: "slides.mp4", FileURL: "videos/slides", Status: model.TaskStatusCompleted, VisualMode: model.VisualModeCaption}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	dispatch, err := repos.PrepareInitialTaskDispatch(repository.InitialTaskDispatchRequest{
		Task: task, AllowedStatuses: []int8{model.TaskStatusCompleted}, JobType: model.TaskJobTypeVisual, Stage: model.TaskStageVisual,
		Now: now, LeaseUntil: now.Add(time.Hour), Token: "first-visual",
	})
	if err != nil {
		t.Fatal(err)
	}
	visualCalls, indexCalls := 0, 0
	consumer := &Consumer{repo: repos}
	consumer.visualIndex = func(ctx context.Context, task *model.VideoTask) (int, error) {
		visualCalls++
		_, err := repos.PublishVisualFrames(repository.TaskProcessingLeaseRequest{TaskID: task.ID, JobType: model.TaskJobTypeVisual, Token: visualprogress.Attempt(ctx), Now: time.Now()},
			[]model.VideoVisualFrame{{TaskID: task.ID, FrameIndex: 0, TimeMs: 1000, VisionCaption: "图表趋势", Status: model.VisualFrameStatusCompleted}},
			repository.VisualProgressUpdate{TotalKnown: true, TotalFrames: 1, Processed: 1})
		return 1, err
	}
	consumer.ragIndex = func(context.Context, *model.VideoTask) error {
		indexCalls++
		if indexCalls == 1 {
			return context.DeadlineExceeded
		}
		return nil
	}
	if err := consumer.handleVisualBuild(context.Background(), RAGIndexPayload{TaskID: task.ID, ClaimToken: dispatch.Token}); err != nil {
		t.Fatal(err)
	}
	failed, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Stage != model.TaskStageIndexing || failed.NextRetryAt == nil {
		t.Fatalf("failure did not preserve index resume point: %+v", failed)
	}
	_, stage := retryDispatchState(model.TaskJobTypeVisual, failed.Stage)
	retryAt := failed.NextRetryAt.Add(time.Second)
	claimed, err := repos.ClaimRetryDispatch(repository.TaskDispatchClaimRequest{TaskID: task.ID, JobType: model.TaskJobTypeVisual, Stage: stage, ExpectedVersion: failed.LeaseVersion, Now: retryAt, LeaseUntil: retryAt.Add(time.Hour), Token: "retry-visual"})
	if err != nil || !claimed {
		t.Fatalf("retry dispatch=%v %v", claimed, err)
	}
	if err := consumer.handleVisualBuild(context.Background(), RAGIndexPayload{TaskID: task.ID, ClaimToken: "retry-visual"}); err != nil {
		t.Fatal(err)
	}
	if visualCalls != 1 || indexCalls != 2 {
		t.Fatalf("repeated expensive visual calls: visual=%d index=%d", visualCalls, indexCalls)
	}
	current, _ := repos.Task.FindByID(task.ID)
	if current.Status != model.TaskStatusCompleted {
		t.Fatalf("retry did not complete: %+v", current)
	}
	// Explicit new builds still start from visual processing, including after an index failure.
	dispatch, err = repos.PrepareInitialTaskDispatch(repository.InitialTaskDispatchRequest{Task: current, AllowedStatuses: []int8{model.TaskStatusCompleted}, JobType: model.TaskJobTypeVisual, Stage: model.TaskStageVisual, Now: time.Now(), LeaseUntil: time.Now().Add(time.Hour), Token: "explicit-new-visual"})
	if err != nil {
		t.Fatal(err)
	}
	if err := consumer.handleVisualBuild(context.Background(), RAGIndexPayload{TaskID: task.ID, ClaimToken: dispatch.Token}); err != nil {
		t.Fatal(err)
	}
	if visualCalls != 2 {
		t.Fatal("explicit rebuild reused old frame processing")
	}
}
