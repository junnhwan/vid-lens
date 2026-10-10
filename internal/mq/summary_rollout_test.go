package mq

import (
	"context"
	"testing"
	"time"

	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

type acceptedSummaryGenerator struct {
	t     *testing.T
	calls int
}

func (g *acceptedSummaryGenerator) Generate(_ context.Context, task *model.VideoTask, job *model.TaskJob, token string) error {
	g.calls++
	if task.UserID != 7 || job.GenerationID != "accepted-generation" || job.InputSourceID != "accepted-source" || job.InputSnapshotJSON != `{"accepted":"immutable"}` || job.ProcessingToken != token || token == "" {
		g.t.Fatal("consumer changed the already accepted frozen input")
	}
	return nil
}

// Admission controls stay outside the consumer. Recovery must complete the
// same accepted dispatch, without re-admission or an extra generation.
func TestSummaryConsumerCompletesAcceptedGenerationAndIgnoresRedelivery(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "accepted-media", Filename: "accepted.mp4", Status: model.TaskStatusCompleted}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.Transcription.Create(&model.VideoTranscription{TaskID: task.ID, FileMD5: task.FileMD5, Content: "already accepted complete source"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	dispatch, err := repos.PrepareInitialTaskDispatch(repository.InitialTaskDispatchRequest{Task: task, AllowedStatuses: []int8{model.TaskStatusCompleted}, JobType: model.TaskJobTypeSummary, Stage: model.TaskStageSummarizing, Token: "accepted-dispatch", Now: now, LeaseUntil: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if err := repos.TaskJob.FreezeSummaryInput(task.ID, "accepted-generation", "accepted-source", `{"accepted":"immutable"}`); err != nil {
		t.Fatal(err)
	}
	generator := &acceptedSummaryGenerator{t: t}
	consumer := &Consumer{repo: repos}
	consumer.SetSummaryGenerator(generator)
	payload := AnalyzePayload{TaskID: task.ID, ClaimToken: dispatch.Token, BudgetID: dispatch.RetryBudgetID}
	if err := consumer.handleTranscriptSummary(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	job, err := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeSummary)
	if err != nil || job.Status != model.TaskStatusCompleted || job.ProcessingToken != "" || job.GenerationID != "accepted-generation" {
		t.Fatalf("accepted generation did not close its same job: %+v %v", job, err)
	}
	if err := consumer.handleTranscriptSummary(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	if generator.calls != 1 {
		t.Fatal("accepted delivery or redelivery generated another result")
	}
}
