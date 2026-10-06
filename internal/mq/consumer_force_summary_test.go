package mq

import (
	"context"
	"testing"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

func TestForcedSummaryOnDuplicateTaskKeepsOriginalAndSavesNewResult(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	md5 := "efefefefefefefefefefefefefefefef"
	original := &model.VideoTask{UserID: 7, FileMD5: md5, Filename: "original.mp4", Status: model.TaskStatusCompleted}
	repeat := &model.VideoTask{UserID: 8, FileMD5: md5, Filename: "repeat.mp4", Status: model.TaskStatusCompleted}
	for _, task := range []*model.VideoTask{original, repeat} {
		if err := repos.Task.Create(task); err != nil {
			t.Fatal(err)
		}
	}
	if err := repos.Transcription.Create(&model.VideoTranscription{TaskID: original.ID, FileMD5: md5, Content: "canonical transcript"}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Summary.Create(&model.AISummary{TaskID: original.ID, FileMD5: md5, Content: "original shared summary"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	dispatch, err := repos.PrepareInitialTaskDispatch(repository.InitialTaskDispatchRequest{
		Task: repeat, AllowedStatuses: []int8{model.TaskStatusCompleted}, JobType: model.TaskJobTypeSummary,
		Stage: model.TaskStageSummarizing, SummaryForce: true, Token: "force-summary", Now: now, LeaseUntil: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	strategy := &summaryPipelineAI{}
	consumer := &Consumer{repo: repos, ai: strategy}
	if err := consumer.handleTranscriptSummary(context.Background(), AnalyzePayload{TaskID: repeat.ID, ClaimToken: dispatch.Token, BudgetID: dispatch.RetryBudgetID}); err != nil {
		t.Fatal(err)
	}
	if len(strategy.calls) != 1 {
		t.Fatalf("model calls=%d, want 1", len(strategy.calls))
	}
	job, err := repos.TaskJob.FindByTaskAndType(repeat.ID, model.TaskJobTypeSummary)
	if err != nil || job == nil || job.Status != model.TaskStatusCompleted {
		t.Fatalf("summary job=%+v err=%v", job, err)
	}
	fresh, err := repos.Summary.FindByTaskID(repeat.ID)
	if err != nil || fresh == nil || fresh.Content == "" {
		t.Fatalf("new task summary=%+v err=%v", fresh, err)
	}
	shared, err := repos.Summary.FindByTaskID(original.ID)
	if err != nil || shared == nil || shared.Content != "original shared summary" {
		t.Fatalf("original summary=%+v err=%v", shared, err)
	}
}

func TestRetranscriptionRequeuesIndexInsteadOfReusingStaleChunks(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	md5 := "abababababababababababababababab"
	task := &model.VideoTask{UserID: 7, FileMD5: md5, Filename: "retranscribe.mp4", Status: model.TaskStatusCompleted}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, FileMD5: md5, Content: "wrong old source"}); err != nil {
		t.Fatal(err)
	}
	finished := time.Now().Add(-time.Hour)
	if err := repos.RAGIndex.Upsert(&model.VideoRAGIndex{UserID: 7, TaskID: task.ID, FileMD5: md5, EmbeddingModel: "embed-a", Status: model.RAGIndexStatusIndexed, BuildVersion: model.CurrentRAGIndexBuildVersion, ChunkerVersion: model.CurrentRAGChunkerVersion, SourceMappingVersion: model.CurrentRAGSourceMappingVersion, FinishedAt: &finished}); err != nil {
		t.Fatal(err)
	}
	if err := repos.VideoChunk.ReplaceTaskChunks(task.ID, "embed-a", []model.VideoChunk{{UserID: 7, TaskID: task.ID, EmbeddingModel: "embed-a", Content: "wrong old source", ContentHash: "old", VectorID: "old-vector"}}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	dispatch, err := repos.PrepareInitialTaskDispatch(repository.InitialTaskDispatchRequest{Task: task, AllowedStatuses: []int8{model.TaskStatusCompleted}, JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing, ResetTranscription: true, Token: "transcribe-dispatch", Now: now, LeaseUntil: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := repos.ClaimTaskProcessing(repository.TaskProcessingClaimRequest{TaskID: task.ID, JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing, MessageToken: dispatch.Token, NewToken: "transcribe-worker", Now: now, LeaseUntil: now.Add(time.Hour)})
	if err != nil || claim.Outcome != repository.TaskLeaseAcquired {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	ctx := withProcessingLeaseOwner(context.Background(), &processingLeaseOwner{repos: repos, taskID: task.ID, jobType: model.TaskJobTypeTranscribe, token: claim.Token})
	producer := &recordingRAGIndexProducer{}
	consumer := &Consumer{repo: repos, ragProducer: producer, profiles: staticProfileResolver{profile: &ai.Profile{EmbeddingProvider: "openai", EmbeddingEndpoint: "https://example.com/v1/embeddings", EmbeddingAPIKey: "fixture-key", EmbeddingDim: 1536, EmbeddingModel: "embed-a"}}}
	if err := consumer.runLeasedSideEffect(ctx, func(r *repository.Repositories) error {
		return r.SaveTranscriptionAndInvalidateIndex(&model.VideoTranscription{TaskID: task.ID, FileMD5: md5, Content: "correct new source"})
	}); err != nil {
		t.Fatal(err)
	}
	chunks, err := repos.VideoChunk.ListByTaskID(7, task.ID, "embed-a")
	if err != nil || len(chunks) != 0 {
		t.Fatalf("stale chunks=%+v err=%v", chunks, err)
	}
	index, err := repos.RAGIndex.FindByTaskAndModel(7, task.ID, "embed-a")
	if err != nil || index == nil || index.Status != model.RAGIndexStatusNeedsRebuild {
		t.Fatalf("stale index=%+v err=%v", index, err)
	}
	enqueued, err := consumer.indexAfterTranscription(ctx, task)
	if err != nil || !enqueued || len(producer.taskIDs) != 1 {
		t.Fatalf("enqueued=%v deliveries=%v err=%v", enqueued, producer.taskIDs, err)
	}
}
