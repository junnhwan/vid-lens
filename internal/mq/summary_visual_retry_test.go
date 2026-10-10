package mq

import (
	"context"
	"testing"
	"time"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
	"vid-lens/internal/repository"
)

type visualRetryConsumerGenerator struct {
	t      *testing.T
	calls  int
	frozen string
}

func (g *visualRetryConsumerGenerator) Generate(_ context.Context, task *model.VideoTask, job *model.TaskJob, token string) error {
	g.calls++
	if task.UserID != 7 || job.GenerationID != "visual-attempt" || job.InputSnapshotJSON != g.frozen || token == "" || token != job.ProcessingToken {
		g.t.Fatal("consumer changed the visual-only attempt authority")
	}
	var snapshot processing.GenerationSnapshot
	if err := artifact.Decode([]byte(job.InputSnapshotJSON), &snapshot); err != nil || snapshot.Operation != processing.OperationVisualRetry || snapshot.VisualRetry == nil || snapshot.VisualRetry.BaseDocumentJSON != "frozen-parent-text" || !snapshot.VisualRetry.NewBudgetAuthorized {
		g.t.Fatal("consumer lost frozen visual-only operation")
	}
	return nil
}

func TestSummaryConsumerRoutesVisualOnlyAttemptAndIgnoresOldDelivery(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "retry-media", Filename: "retry.mp4", Status: model.TaskStatusCompleted}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.Transcription.Create(&model.VideoTranscription{TaskID: task.ID, FileMD5: task.FileMD5, Content: "stored source, no ASR call"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	dispatch, err := repos.PrepareInitialTaskDispatch(repository.InitialTaskDispatchRequest{Task: task, AllowedStatuses: []int8{model.TaskStatusCompleted}, JobType: model.TaskJobTypeSummary, Stage: model.TaskStageSummarizing, Token: "visual-dispatch", Now: now, LeaseUntil: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	frozen := artifact.JSON(processing.GenerationSnapshot{Operation: processing.OperationVisualRetry, VisualRetry: &processing.VisualRetrySnapshot{ParentGenerationID: "previous-generation", BaseDocumentJSON: "frozen-parent-text", NewBudgetAuthorized: true}})
	if err = repos.TaskJob.FreezeSummaryInput(task.ID, "visual-attempt", "frozen-source", frozen); err != nil {
		t.Fatal(err)
	}
	generator := &visualRetryConsumerGenerator{t: t, frozen: frozen}
	consumer := &Consumer{repo: repos}
	consumer.SetSummaryGenerator(generator)
	payload := AnalyzePayload{TaskID: task.ID, ClaimToken: dispatch.Token, BudgetID: dispatch.RetryBudgetID}
	stale := payload
	stale.ClaimToken = "old-generation-token"
	if err = consumer.handleTranscriptSummary(context.Background(), stale); err != nil {
		t.Fatal(err)
	}
	if generator.calls != 0 {
		t.Fatal("old generation message executed visual retry")
	}
	if err = consumer.handleTranscriptSummary(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	if err = consumer.handleTranscriptSummary(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	job, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeSummary)
	if generator.calls != 1 || job.Status != model.TaskStatusCompleted || job.InputSnapshotJSON != frozen || job.GenerationID != "visual-attempt" {
		t.Fatal("redelivery recreated/replaced the accepted visual attempt")
	}
}
