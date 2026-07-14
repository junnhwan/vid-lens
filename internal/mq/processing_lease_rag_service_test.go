package mq

import (
	"context"
	"errors"
	"testing"
	"time"

	"vid-lens/internal/model"
	"vid-lens/internal/pkg/processingguard"
	"vid-lens/internal/repository"
)

func TestRAGIndexerContextRevalidatesConsumerLeaseInsideServiceWork(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Now()
	task := &model.VideoTask{
		UserID: 35, FileMD5: "35353535353535353535353535353535", Filename: "rag-service-guard.mp4",
		Status: model.TaskStatusPending, Stage: model.TaskStageIndexing,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "transcript"}); err != nil {
		t.Fatal(err)
	}

	sideEffects := 0
	consumer := &Consumer{
		repo: repos, now: func() time.Time { return now }, newToken: func() string { return "old-worker" },
		processingLease: time.Minute, leaseHeartbeatInterval: time.Hour,
	}
	consumer.ragIndex = func(ctx context.Context, current *model.VideoTask) error {
		claim, err := repos.ClaimTaskProcessing(repository.TaskProcessingClaimRequest{
			TaskID: current.ID, JobType: model.TaskJobTypeRAGIndex, Stage: model.TaskStageIndexing,
			Now: now.Add(2 * time.Minute), LeaseUntil: now.Add(3 * time.Minute), NewToken: "new-worker",
		})
		if err != nil {
			return err
		}
		if claim.Outcome != repository.TaskLeaseAcquired {
			return errors.New("new worker did not acquire expired lease")
		}
		if err := processingguard.Check(ctx); err != nil {
			return err
		}
		sideEffects++
		return nil
	}

	err := consumer.handleRAGIndex(context.Background(), ragIndexMessage(task.ID, "trace-rag-service-fence"))
	if !errors.Is(err, ErrProcessingLeaseLost) {
		t.Fatalf("error = %v, want ErrProcessingLeaseLost", err)
	}
	if sideEffects != 0 {
		t.Fatalf("guarded side effects = %d, want 0 after lease loss", sideEffects)
	}
}
