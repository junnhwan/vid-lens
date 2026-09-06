package mq

import (
	"context"
	"testing"
	"time"

	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

// 回归现场（真机验收 task 37）：转写 job 尾部先把最终完成权交给 rag job
// （running/indexing），而 rag job 在转写 job 仍持有 processing lease 期间
// 已完成索引构建，其 CompleteTaskProcessing 被 CAS 拒绝后静默放弃——之后
// 无人完成，任务永久卡在 running/indexing。本组测试锁定转写完成边界的
// 三种分支：rag 已交付 → 直接完成；rag 从未入队 → 直接完成；rag 消息在途 →
// 交接（且调度器可回收丢失的 rag 消息）。

func newTranscribeCompletionFixture(t *testing.T) (*repository.Repositories, *Consumer, *model.VideoTask, string, time.Time) {
	t.Helper()
	repos := newConsumerTestRepositories(t)
	now := time.Date(2026, 9, 6, 15, 0, 0, 0, time.UTC)
	task := &model.VideoTask{
		UserID: 37, FileMD5: "37373737373737373737373737373737", Filename: "race.mp4",
		Status: model.TaskStatusRunning, Stage: model.TaskStageTranscribing, MaxRetries: 3,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "transcript", Words: 2}); err != nil {
		t.Fatalf("upsert transcription: %v", err)
	}
	consumer := &Consumer{repo: repos, processingLease: time.Hour, now: func() time.Time { return now }, newToken: func() string { return "transcribe-worker" }}
	claim, err := consumer.claimTaskForMessage(task.ID, TaskJobTranscribe, model.TaskStageTranscribing, "")
	if err != nil || claim.Outcome != repository.TaskLeaseAcquired {
		t.Fatalf("transcribe claim = %+v/%v", claim, err)
	}
	return repos, consumer, task, claim.Token, now
}

func TestTranscribeCompletionCompletesTaskWhenRAGIndexAlreadyDelivered(t *testing.T) {
	repos, consumer, task, transcribeToken, now := newTranscribeCompletionFixture(t)

	// rag 消息在途：UpsertQueued 建立 rag job 行，rag job 随后消费。
	if err := repos.TaskJob.UpsertQueued(task, TaskJobRAGIndex, model.TaskStageIndexing, task.MaxRetries); err != nil {
		t.Fatalf("upsert queued rag job: %v", err)
	}

	// rag job 在转写 job 持锁期间完成索引构建（ragIndexBuild.complete 的落库）。
	if err := repos.RAGIndex.Upsert(&model.VideoRAGIndex{
		UserID: task.UserID, TaskID: task.ID, FileMD5: task.FileMD5,
		EmbeddingModel: "bge-m3", EmbeddingDim: 1024,
		Status: model.RAGIndexStatusIndexed, ChunkCount: 12,
		BuildVersion:         model.CurrentRAGIndexBuildVersion,
		ChunkerVersion:       model.CurrentRAGChunkerVersion,
		SourceMappingVersion: model.CurrentRAGSourceMappingVersion,
		StartedAt:            &now, FinishedAt: &now,
	}); err != nil {
		t.Fatalf("upsert indexed rag row: %v", err)
	}

	// rag job 的完成被 ownsProcessingLease CAS 拒绝（转写 job 持锁）→ completed=false。
	ragCompleted, err := consumer.completeTaskProcessing(repository.TaskProcessingCompleteRequest{
		TaskID: task.ID, JobType: TaskJobRAGIndex, JobStage: model.TaskStageIndexing, Token: "rag-worker",
		TaskStatus: model.TaskStatusCompleted, TaskStage: model.TaskStageNone, Now: now,
	})
	if err != nil {
		t.Fatalf("rag completion attempt: %v", err)
	}
	if ragCompleted {
		t.Fatalf("rag completion should be rejected while transcribe holds the lease")
	}

	// 转写尾部完成：索引已交付 → 直接完成任务，而不是交接给一个不会再来
	// 的 rag job（修复前这里置 running/indexing 后永久卡死）。
	if err := consumer.completeTranscribeAfterIndex(context.Background(), task, transcribeToken, true); err != nil {
		t.Fatalf("completeTranscribeAfterIndex: %v", err)
	}
	current, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != model.TaskStatusCompleted || current.Stage != model.TaskStageNone {
		t.Fatalf("task after transcribe completion = status:%d stage:%s, want completed/none", current.Status, current.Stage)
	}
	if current.FinishedAt == nil {
		t.Fatalf("task finished_at not set")
	}
	if current.LastJobType != TaskJobTranscribe {
		t.Fatalf("last_job_type = %s, want unchanged transcribe", current.LastJobType)
	}
}

func TestTranscribeCompletionCompletesTaskWhenRAGNeverEnqueued(t *testing.T) {
	repos, consumer, task, transcribeToken, _ := newTranscribeCompletionFixture(t)

	// indexAfterTranscription 的去重命中（跨 task 复用已有 indexed 行）或
	// ragProducer 缺失 → ragEnqueued=false。修复前仍置 running/indexing 交接
	// 给永不运行的 rag job。
	if err := consumer.completeTranscribeAfterIndex(context.Background(), task, transcribeToken, false); err != nil {
		t.Fatalf("completeTranscribeAfterIndex: %v", err)
	}
	current, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != model.TaskStatusCompleted || current.Stage != model.TaskStageNone {
		t.Fatalf("task after completion = status:%d stage:%s, want completed/none", current.Status, current.Stage)
	}
}

func TestTranscribeCompletionHandsOffToInFlightRAGMessage(t *testing.T) {
	repos, consumer, task, transcribeToken, _ := newTranscribeCompletionFixture(t)

	// rag 消息在途：UpsertQueued 建立 rag job 行，rag job 随后消费并完成任务。
	if err := repos.TaskJob.UpsertQueued(task, TaskJobRAGIndex, model.TaskStageIndexing, task.MaxRetries); err != nil {
		t.Fatalf("upsert queued rag job: %v", err)
	}

	if err := consumer.completeTranscribeAfterIndex(context.Background(), task, transcribeToken, true); err != nil {
		t.Fatalf("completeTranscribeAfterIndex: %v", err)
	}
	current, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != model.TaskStatusRunning || current.Stage != model.TaskStageIndexing {
		t.Fatalf("task after handoff = status:%d stage:%s, want running/indexing", current.Status, current.Stage)
	}
	if current.ProcessingToken != "" || current.LeaseKind != "" || current.LeaseExpiresAt != nil {
		t.Fatalf("handoff must release the processing lease: %+v", current)
	}
}
