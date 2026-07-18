package mq

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

type countingTranscriber struct{ calls int }

func (a *countingTranscriber) Transcribe(context.Context, string) (string, error) {
	a.calls++
	return "should-not-run", nil
}
func (a *countingTranscriber) TranscribeChunks(context.Context, []string) (string, error) {
	return "", nil
}
func (a *countingTranscriber) Summarize(context.Context, string) (string, error) { return "", nil }

var _ ai.Strategy = (*countingTranscriber)(nil)

func TestLostProcessingLeaseStopsBeforeNextAIChunk(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Now()
	leaseUntil := now.Add(time.Minute)
	task := &model.VideoTask{
		UserID: 20, FileMD5: "ababababababababababababababab20", Filename: "lost.mp4",
		Status: model.TaskStatusRunning, Stage: model.TaskStageTranscribing, LastJobType: model.TaskJobTypeTranscribe,
		ProcessingToken: "new-worker", LeaseKind: model.TaskLeaseKindProcessing, LeaseExpiresAt: &leaseUntil, LeaseVersion: 2,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.TaskJob.UpsertDispatching(task, model.TaskJobTypeTranscribe, model.TaskStatusRunning, model.TaskStageTranscribing); err != nil {
		t.Fatal(err)
	}

	consumer := &Consumer{repo: repos}
	consumer.splitAudio = func(context.Context, string, string, int) ([]string, error) { return []string{"chunk-1.wav"}, nil }
	ctx := withProcessingLeaseOwner(context.Background(), &processingLeaseOwner{
		repos: repos, taskID: task.ID, jobType: model.TaskJobTypeTranscribe, token: "old-worker", now: time.Now,
	})
	strategy := &countingTranscriber{}
	_, err := consumer.transcribeAudio(ctx, task.ID, "audio.wav", strategy)
	if !errors.Is(err, ErrProcessingLeaseLost) {
		t.Fatalf("error = %v, want ErrProcessingLeaseLost", err)
	}
	if strategy.calls != 0 {
		t.Fatalf("ASR calls = %d, want 0", strategy.calls)
	}
}

func TestRecordLeasedFailureUsesJobRetryBudgetNotParentBudget(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	leaseUntil := now.Add(time.Minute)
	task := &model.VideoTask{
		UserID: 21, FileMD5: "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcd21", Filename: "isolated-budget.mp4",
		Status: model.TaskStatusRunning, Stage: model.TaskStageIndexing, LastJobType: model.TaskJobTypeRAGIndex,
		RetryCount: 3, MaxRetries: 3, ProcessingToken: "rag-worker", LeaseKind: model.TaskLeaseKindProcessing,
		LeaseExpiresAt: &leaseUntil, LeaseVersion: 1,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	// A newly-created RAG stage starts at zero even though an earlier stage exhausted the compatibility mirror.
	if err := repos.TaskJob.UpsertDispatching(task, model.TaskJobTypeRAGIndex, model.TaskStatusRunning, model.TaskStageIndexing); err != nil {
		t.Fatal(err)
	}

	consumer := &Consumer{repo: repos, now: func() time.Time { return now }}
	consumer.SetRetryPolicy(TaskRetryPolicy{MaxRetries: 3, BackoffSeconds: []int{60}, Now: func() time.Time { return now }})
	if err := consumer.recordTaskFailure(task.ID, TaskJobRAGIndex, model.TaskStageIndexing, errors.New("network timeout"), "rag-worker"); err != nil {
		t.Fatal(err)
	}

	current, _ := repos.Task.FindByID(task.ID)
	job, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeRAGIndex)
	if current.Status != model.TaskStatusFailed || current.RetryCount != 1 {
		t.Fatalf("parent = %+v", current)
	}
	if job == nil || job.Status != model.TaskStatusFailed || job.RetryCount != 1 {
		t.Fatalf("rag job = %+v", job)
	}
}

func TestProcessingLeaseHeartbeatRenewsBeforeOriginalExpiry(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Now()
	originalExpiry := now.Add(80 * time.Millisecond)
	task := &model.VideoTask{
		UserID: 22, FileMD5: "efefefefefefefefefefefefefefef22", Filename: "heartbeat.mp4",
		Status: model.TaskStatusRunning, Stage: model.TaskStageSummarizing, LastJobType: model.TaskJobTypeAnalyze,
		ProcessingToken: "worker", LeaseKind: model.TaskLeaseKindProcessing, LeaseExpiresAt: &originalExpiry, LeaseVersion: 1,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.TaskJob.UpsertDispatching(task, model.TaskJobTypeAnalyze, model.TaskStatusRunning, model.TaskStageSummarizing); err != nil {
		t.Fatal(err)
	}

	consumer := &Consumer{repo: repos, processingLease: 80 * time.Millisecond, leaseHeartbeatInterval: 15 * time.Millisecond}
	ctx, stop := consumer.startProcessingLeaseHeartbeat(context.Background(), task.ID, model.TaskJobTypeAnalyze, "worker")
	defer stop()
	select {
	case <-ctx.Done():
		t.Fatalf("heartbeat lost valid lease: %v", context.Cause(ctx))
	case <-time.After(45 * time.Millisecond):
	}
	current, _ := repos.Task.FindByID(task.ID)
	if current.LeaseExpiresAt == nil || !current.LeaseExpiresAt.After(originalExpiry) {
		t.Fatalf("lease was not renewed: old=%v new=%v", originalExpiry, current.LeaseExpiresAt)
	}
}

type leaseCallbackStrategy struct {
	transcribe func() (string, error)
	summarize  func() (string, error)
}

func (s *leaseCallbackStrategy) Transcribe(context.Context, string) (string, error) {
	if s.transcribe == nil {
		return "", nil
	}
	return s.transcribe()
}

func (s *leaseCallbackStrategy) TranscribeChunks(context.Context, []string) (string, error) {
	return "", nil
}

func (s *leaseCallbackStrategy) Summarize(context.Context, string) (string, error) {
	if s.summarize == nil {
		return "", nil
	}
	return s.summarize()
}

func TestLostLeaseAfterSuccessfulASRDoesNotOverwriteNewChunkResult(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Now()
	leaseUntil := now.Add(time.Minute)
	task := &model.VideoTask{
		UserID: 30, FileMD5: "30303030303030303030303030303030", Filename: "asr-success.mp4",
		Status: model.TaskStatusRunning, Stage: model.TaskStageTranscribing, LastJobType: model.TaskJobTypeTranscribe,
		ProcessingToken: "old-worker", LeaseKind: model.TaskLeaseKindProcessing, LeaseExpiresAt: &leaseUntil, LeaseVersion: 1,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.TaskJob.UpsertDispatching(task, model.TaskJobTypeTranscribe, model.TaskStatusRunning, model.TaskStageTranscribing); err != nil {
		t.Fatal(err)
	}
	leaseNow := now
	ctx := withProcessingLeaseOwner(context.Background(), &processingLeaseOwner{repos: repos, taskID: task.ID, jobType: model.TaskJobTypeTranscribe, token: "old-worker", now: func() time.Time { return leaseNow }})
	strategy := &leaseCallbackStrategy{transcribe: func() (string, error) {
		if err := repos.TranscriptionChunk.UpsertCompleted(task.ID, 0, "chunk-1.wav", "new-worker-result"); err != nil {
			t.Fatal(err)
		}
		leaseNow = leaseUntil.Add(time.Second)
		return "stale-worker-result", nil
	}}
	consumer := &Consumer{repo: repos, splitAudio: func(context.Context, string, string, int) ([]string, error) { return []string{"chunk-1.wav"}, nil }}

	_, err := consumer.transcribeAudio(ctx, task.ID, "audio.wav", strategy)
	if !errors.Is(err, ErrProcessingLeaseLost) {
		t.Fatalf("error = %v, want ErrProcessingLeaseLost", err)
	}
	chunk, err := repos.TranscriptionChunk.FindByTaskAndIndex(task.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if chunk == nil || chunk.Status != model.TranscriptionChunkStatusCompleted || chunk.Content != "new-worker-result" {
		t.Fatalf("chunk = %+v, want newer completed result", chunk)
	}
}

func TestLostLeaseAfterFailedASRDoesNotReplaceNewChunkWithFailure(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Now()
	leaseUntil := now.Add(time.Minute)
	task := &model.VideoTask{
		UserID: 31, FileMD5: "31313131313131313131313131313131", Filename: "asr-failure.mp4",
		Status: model.TaskStatusRunning, Stage: model.TaskStageTranscribing, LastJobType: model.TaskJobTypeTranscribe,
		ProcessingToken: "old-worker", LeaseKind: model.TaskLeaseKindProcessing, LeaseExpiresAt: &leaseUntil, LeaseVersion: 1,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.TaskJob.UpsertDispatching(task, model.TaskJobTypeTranscribe, model.TaskStatusRunning, model.TaskStageTranscribing); err != nil {
		t.Fatal(err)
	}
	leaseNow := now
	ctx := withProcessingLeaseOwner(context.Background(), &processingLeaseOwner{repos: repos, taskID: task.ID, jobType: model.TaskJobTypeTranscribe, token: "old-worker", now: func() time.Time { return leaseNow }})
	providerErr := errors.New("old provider failed")
	strategy := &leaseCallbackStrategy{transcribe: func() (string, error) {
		if err := repos.TranscriptionChunk.UpsertCompleted(task.ID, 0, "chunk-1.wav", "new-worker-result"); err != nil {
			t.Fatal(err)
		}
		leaseNow = leaseUntil.Add(time.Second)
		return "", providerErr
	}}
	consumer := &Consumer{repo: repos, splitAudio: func(context.Context, string, string, int) ([]string, error) { return []string{"chunk-1.wav"}, nil }}

	_, err := consumer.transcribeAudio(ctx, task.ID, "audio.wav", strategy)
	if !errors.Is(err, ErrProcessingLeaseLost) {
		t.Fatalf("error = %v, want ErrProcessingLeaseLost", err)
	}
	chunk, err := repos.TranscriptionChunk.FindByTaskAndIndex(task.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if chunk == nil || chunk.Status != model.TranscriptionChunkStatusCompleted || chunk.Content != "new-worker-result" {
		t.Fatalf("chunk = %+v, want newer completed result", chunk)
	}
}

func TestLostLeaseAfterSummaryDoesNotOverwriteNewSummary(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Now()
	leaseUntil := now.Add(time.Minute)
	task := &model.VideoTask{
		UserID: 32, FileMD5: "32323232323232323232323232323232", Filename: "summary.mp4",
		Status: model.TaskStatusRunning, Stage: model.TaskStageSummarizing, LastJobType: model.TaskJobTypeAnalyze,
		ProcessingToken: "old-worker", LeaseKind: model.TaskLeaseKindProcessing, LeaseExpiresAt: &leaseUntil, LeaseVersion: 1,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.TaskJob.UpsertDispatching(task, model.TaskJobTypeAnalyze, model.TaskStatusRunning, model.TaskStageSummarizing); err != nil {
		t.Fatal(err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "transcript"}); err != nil {
		t.Fatal(err)
	}
	leaseNow := now
	ctx := withProcessingLeaseOwner(context.Background(), &processingLeaseOwner{repos: repos, taskID: task.ID, jobType: model.TaskJobTypeAnalyze, token: "old-worker", now: func() time.Time { return leaseNow }})
	strategy := &leaseCallbackStrategy{summarize: func() (string, error) {
		if err := repos.Summary.Upsert(&model.AISummary{TaskID: task.ID, Content: "new-worker-summary"}); err != nil {
			t.Fatal(err)
		}
		leaseNow = leaseUntil.Add(time.Second)
		return "stale-worker-summary", nil
	}}
	consumer := &Consumer{repo: repos, ai: strategy}

	err := consumer.summarizeTask(ctx, task)
	if !errors.Is(err, ErrProcessingLeaseLost) {
		t.Fatalf("error = %v, want ErrProcessingLeaseLost", err)
	}
	summary, err := repos.Summary.FindByTaskID(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if summary == nil || summary.Content != "new-worker-summary" {
		t.Fatalf("summary = %+v, want newer result", summary)
	}
}

func TestLostLeaseAfterTitleCallDoesNotOverwriteNewTitle(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Now()
	leaseUntil := now.Add(time.Minute)
	task := &model.VideoTask{
		UserID: 33, FileMD5: "33333333333333333333333333333333", Filename: "title.mp4",
		Status: model.TaskStatusRunning, Stage: model.TaskStageSummarizing, LastJobType: model.TaskJobTypeAnalyze,
		ProcessingToken: "old-worker", LeaseKind: model.TaskLeaseKindProcessing, LeaseExpiresAt: &leaseUntil, LeaseVersion: 1,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.TaskJob.UpsertDispatching(task, model.TaskJobTypeAnalyze, model.TaskStatusRunning, model.TaskStageSummarizing); err != nil {
		t.Fatal(err)
	}
	var clock atomic.Int64
	clock.Store(now.UnixNano())
	callbackErr := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callbackErr <- repos.Task.UpdateTitle(task.ID, "new-worker-title")
		clock.Store(leaseUntil.Add(time.Second).UnixNano())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"stale-worker-title"}}]}`))
	}))
	defer server.Close()
	ctx := withProcessingLeaseOwner(context.Background(), &processingLeaseOwner{repos: repos, taskID: task.ID, jobType: model.TaskJobTypeAnalyze, token: "old-worker", now: func() time.Time { return time.Unix(0, clock.Load()) }})
	consumer := &Consumer{
		repo: repos, aiFactory: ai.NewFactory(),
		profiles: staticProfileResolver{profile: &ai.Profile{LLMProvider: "openai_compatible", LLMBaseURL: server.URL, LLMModel: "test-model"}},
	}

	consumer.generateTitle(ctx, task, "transcript")
	if err := <-callbackErr; err != nil {
		t.Fatal(err)
	}
	current, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Title != "new-worker-title" {
		t.Fatalf("title = %q, want newer title", current.Title)
	}
}

func TestLostLeaseAfterRAGIndexerIsReportedAndNotCompleted(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Now()
	task := &model.VideoTask{
		UserID: 34, FileMD5: "34343434343434343434343434343434", Filename: "rag.mp4",
		Status: model.TaskStatusPending, Stage: model.TaskStageIndexing,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "transcript"}); err != nil {
		t.Fatal(err)
	}
	consumer := &Consumer{
		repo: repos, now: func() time.Time { return now }, newToken: func() string { return "old-worker" },
		processingLease: time.Minute, leaseHeartbeatInterval: time.Hour,
	}
	consumer.ragIndex = func(context.Context, *model.VideoTask) error {
		claim, err := repos.ClaimTaskProcessing(repository.TaskProcessingClaimRequest{
			TaskID: task.ID, JobType: model.TaskJobTypeRAGIndex, Stage: model.TaskStageIndexing,
			Now: now.Add(2 * time.Minute), LeaseUntil: now.Add(3 * time.Minute), NewToken: "new-worker",
		})
		if err != nil {
			return err
		}
		if claim.Outcome != repository.TaskLeaseAcquired {
			return errors.New("new worker did not acquire expired lease")
		}
		return nil
	}

	err := consumer.handleRAGIndex(context.Background(), ragIndexMessage(task.ID, "trace-rag-fence"))
	if !errors.Is(err, ErrProcessingLeaseLost) {
		t.Fatalf("error = %v, want ErrProcessingLeaseLost", err)
	}
	current, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	job, err := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeRAGIndex)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status == model.TaskStatusCompleted || job == nil || job.Status == model.TaskStatusCompleted || current.ProcessingToken != "new-worker" {
		t.Fatalf("task = %+v, job = %+v", current, job)
	}
}

func TestRunLeasedSideEffectDoesNotExecuteAfterOwnershipLoss(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Now()
	leaseUntil := now.Add(time.Minute)
	task := &model.VideoTask{
		UserID: 36, FileMD5: "36363636363636363636363636363636", Filename: "fenced-write.mp4",
		Status: model.TaskStatusRunning, Stage: model.TaskStageSummarizing, LastJobType: model.TaskJobTypeAnalyze,
		ProcessingToken: "new-worker", LeaseKind: model.TaskLeaseKindProcessing, LeaseExpiresAt: &leaseUntil, LeaseVersion: 2,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.TaskJob.UpsertDispatching(task, model.TaskJobTypeAnalyze, model.TaskStatusRunning, model.TaskStageSummarizing); err != nil {
		t.Fatal(err)
	}
	ctx := withProcessingLeaseOwner(context.Background(), &processingLeaseOwner{
		repos: repos, taskID: task.ID, jobType: model.TaskJobTypeAnalyze, token: "old-worker", now: func() time.Time { return now },
	})
	consumer := &Consumer{repo: repos}
	calls := 0
	err := consumer.runLeasedSideEffect(ctx, func(*repository.Repositories) error {
		calls++
		return nil
	})
	if !errors.Is(err, ErrProcessingLeaseLost) {
		t.Fatalf("error = %v, want ErrProcessingLeaseLost", err)
	}
	if calls != 0 {
		t.Fatalf("side effect calls = %d, want 0", calls)
	}
}
