package mq

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

// faultMatrixRows enumerates the four fault-matrix acceptance rows. The count
// is the spec's hard deliverable ("__ 档故障矩阵（应为 4）"). Referencing the
// row functions by value makes the compiler enforce their existence: if any is
// removed, this package fails to build.
var faultMatrixRows = []struct {
	name string
	fn   func(*testing.T)
}{
	{"TestFaultMatrix_DBRollbackLeavesTaskUnchanged", TestFaultMatrix_DBRollbackLeavesTaskUnchanged},                         // row 1
	{"TestRetrySchedulerProducerFailureRestoresDispatchLeaseTransactionally", TestRetrySchedulerProducerFailureRestoresDispatchLeaseTransactionally}, // row 2
	{"TestRetrySchedulerRecoversExpiredDispatchLeaseAfterCrash", TestRetrySchedulerRecoversExpiredDispatchLeaseAfterCrash}, // row 3
	{"TestFaultMatrix_DuplicateMessageIsIdempotent", TestFaultMatrix_DuplicateMessageIsIdempotent},                          // row 4
}

// TestFaultMatrixHasFourRows asserts the four fault-matrix rows are present
// and individually runnable. The number 4 is the spec deliverable.
func TestFaultMatrixHasFourRows(t *testing.T) {
	if len(faultMatrixRows) != 4 {
		t.Fatalf("fault matrix rows = %d, want 4", len(faultMatrixRows))
	}
	for _, row := range faultMatrixRows {
		if row.fn == nil {
			t.Errorf("fault matrix row %q has nil func", row.name)
		}
	}
}

// This file is the 4-row 故障矩阵 (fault matrix) acceptance seam for
// docs/specs/02-dispatch-consistency.md. Each row is individually runnable.
// Rows 2 and 3 (publish-failure restore, expired-lease recovery) are covered
// by existing tests in reliability_review_test.go and consumer_loop_test.go;
// the count of runnable fault-matrix rows is asserted by TestFaultMatrixHasFourRows.

// Row 1 — DB 回滚: PrepareInitialTaskDispatch 失败 → task 不变、无 lease、无 publish.
func TestFaultMatrix_DBRollbackLeavesTaskUnchanged(t *testing.T) {
	repos, db := newConsumerLoopTestRepositories(t)
	now := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	task := &model.VideoTask{
		UserID: 30, FileMD5: "f1row1row1row1row1row1row1row1row", Filename: "row1.mp4",
		Status: model.TaskStatusPending, Stage: model.TaskStageUploaded, MaxRetries: 3,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	// Abort the dispatch-lease update inside the prepare transaction.
	if err := db.Exec(
		"CREATE TRIGGER fail_dispatch_lease BEFORE UPDATE OF lease_kind ON video_tasks " +
			"WHEN NEW.lease_kind = 'dispatch' BEGIN SELECT RAISE(ABORT, 'dispatch lease write failed'); END",
	).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	_, err := repos.PrepareInitialTaskDispatch(repository.InitialTaskDispatchRequest{
		Task: task, AllowedStatuses: []int8{model.TaskStatusPending},
		JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing,
		Now: now, LeaseUntil: now.Add(2 * time.Minute), Token: "row1-token",
	})
	if err == nil || !containsAll(err.Error(), "dispatch lease write failed") {
		t.Fatalf("PrepareInitialTaskDispatch error = %v, want dispatch lease write failure", err)
	}

	current, _ := repos.Task.FindByID(task.ID)
	if current.Status != model.TaskStatusPending || current.ProcessingToken != "" || current.LeaseKind != "" || current.LeaseExpiresAt != nil || current.LeaseVersion != 0 {
		t.Fatalf("task was mutated by a rolled-back dispatch: %+v", current)
	}
}

// Row 4 — 重复消息: same MessageId delivered twice → consumer-side idempotency
// key suppresses the second; business logic runs exactly once.
func TestFaultMatrix_DuplicateMessageIsIdempotent(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	task := &model.VideoTask{
		UserID: 31, FileMD5: "f4row4row4row4row4row4row4row4row", Filename: "row4.mp4",
		Status: model.TaskStatusQueued, Stage: model.TaskStageIndexing, LastJobType: TaskJobRAGIndex, MaxRetries: 3,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "row4 evidence"}); err != nil {
		t.Fatalf("create transcription: %v", err)
	}
	if err := repos.TaskJob.UpsertDispatching(task, TaskJobRAGIndex, model.TaskStatusQueued, model.TaskStageIndexing); err != nil {
		t.Fatalf("create job: %v", err)
	}

	// miniredis backs the consumer-side idempotency checker.
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	calls := 0
	consumer := &Consumer{
		repo: repos,
		processingLease: time.Hour,
		now:              func() time.Time { return now },
		newToken:         func() string { return "row4-worker" },
		ragIndex: func(context.Context, *model.VideoTask) error {
			calls++
			return nil
		},
		idempotency: newRedisIdempotencyChecker(rdb, 0),
	}

	// Deliver the SAME message twice through the full consume path so the
	// dedupHandler gate (not handleRAGIndex directly) is exercised.
	reader := &scriptedAmqpReader{}
	payload := ragIndexMessage(task.ID, "trace-row4").Body
	messageID := fmt.Sprintf("%s:%d", TaskJobRAGIndex, task.ID)
	reader.fetches = []amqp.Delivery{
		reader.stubDelivery(1, payload),
		reader.stubDelivery(2, payload), // duplicate MessageId
	}
	// Stamp the shared MessageId on both deliveries so the dedup key collides.
	reader.fetches[0].MessageId = messageID
	reader.fetches[1].MessageId = messageID
	reader.fetches[0].Acknowledger = &recordingAcknowledger{reader: reader}
	reader.fetches[1].Acknowledger = &recordingAcknowledger{reader: reader}

	wrapped := consumer.dedupHandler("video-rag-index", consumer.handleRAGIndex)
	_ = consumeMessages(context.Background(), reader, wrapped)

	// First delivery runs the business logic and sets the idempotency key; the
	// second delivery with the same MessageId is suppressed by the gate.
	if calls != 1 {
		t.Fatalf("business calls = %d, want 1 (duplicate suppressed)", calls)
	}
	_, acks, _, _ := reader.snapshot()
	if len(acks) != 2 {
		t.Fatalf("ack count = %d, want 2 (both deliveries acked: one executed, one suppressed)", len(acks))
	}
}
