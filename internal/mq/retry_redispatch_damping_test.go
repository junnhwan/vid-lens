package mq

import (
	"context"
	"testing"
	"time"

	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

// 回归现场（真机验收缺陷 2）：video-transcribe prefetch=1 时排队消息的
// dispatch lease 反复过期 → RetryScheduler 每个 lease 周期重发一条同 MessageId
// 的重复消息（队列 3→10 条堆积），且排队任务的 stage 被提前写成 transcribing
// （started_at 仍为 NULL），前端无法区分排队中/处理中。本组测试锁定：
// 1) 过期 dispatch lease 的补投被 RedispatchBackoff 阻尼；
// 2) 调度器补投不再改写 task 的 stage（stage 由 worker claim 时推进）。

func createExpiredDispatchTask(t *testing.T, repos *repository.Repositories, now time.Time, userID int64, md5 string) *model.VideoTask {
	t.Helper()
	expired := now.Add(-time.Second)
	task := &model.VideoTask{
		UserID:          userID,
		FileMD5:         md5,
		Filename:        "queued.mp4",
		Status:          model.TaskStatusQueued,
		Stage:           model.TaskStageUploaded,
		MaxRetries:      3,
		LastJobType:     TaskJobTranscribe,
		ProcessingToken: "dispatch-token",
		LeaseKind:       model.TaskLeaseKindDispatch,
		LeaseExpiresAt:  &expired,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create queued task: %v", err)
	}
	// UpsertQueued copies the task's dispatch lease fields onto the job row,
	// mirroring indexAfterTranscription / initial dispatch state.
	if err := repos.TaskJob.UpsertQueued(task, TaskJobTranscribe, model.TaskStageTranscribing, task.MaxRetries); err != nil {
		t.Fatalf("create queued job: %v", err)
	}
	return task
}

func TestRetrySchedulerDampsExpiredDispatchLeaseRedispatch(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Date(2026, 9, 6, 16, 0, 0, 0, time.UTC)
	task := createExpiredDispatchTask(t, repos, now, 21, "21212121212121212121212121212121")
	producer := &leaseCapturingRetryProducer{}
	scheduler := NewRetryScheduler(repos, producer, RetrySchedulerConfig{
		BatchSize: 10, DispatchLease: 2 * time.Minute, RedispatchBackoff: 5 * time.Minute,
		Now: func() time.Time { return now }, NewToken: func() string { return "redispatch-token" },
	})

	// 第一次补投：lease 已过期，正常重发。
	if err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}
	if len(producer.tasks) != 1 || producer.tasks[0] != task.ID {
		t.Fatalf("first redispatch tasks = %v, want [%d]", producer.tasks, task.ID)
	}
	job, _ := repos.TaskJob.FindByTaskAndType(task.ID, TaskJobTranscribe)
	if job == nil || job.NextRetryAt == nil || !job.NextRetryAt.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("job damping next_retry_at = %+v, want %v", job, now.Add(5*time.Minute))
	}

	// lease 已再次过期但阻尼窗口未到：不重发（修复前每个 lease 周期都会重发一条）。
	dampedNow := now.Add(3 * time.Minute)
	scheduler.config.Now = func() time.Time { return dampedNow }
	if err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatalf("damped RunOnce: %v", err)
	}
	if len(producer.tasks) != 1 {
		t.Fatalf("damped redispatch tasks = %v, want no duplicate", producer.tasks)
	}

	// 阻尼窗口过后：恢复补投。
	dueNow := now.Add(6 * time.Minute)
	scheduler.config.Now = func() time.Time { return dueNow }
	if err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatalf("post-backoff RunOnce: %v", err)
	}
	if len(producer.tasks) != 2 {
		t.Fatalf("post-backoff redispatch tasks = %v, want a second dispatch", producer.tasks)
	}
}

func TestRetrySchedulerRedispatchKeepsQueuedTaskStage(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Date(2026, 9, 6, 17, 0, 0, 0, time.UTC)
	task := createExpiredDispatchTask(t, repos, now, 22, "22222222222222222222222222222222")
	producer := &leaseCapturingRetryProducer{}
	scheduler := NewRetryScheduler(repos, producer, RetrySchedulerConfig{
		BatchSize: 10, DispatchLease: 2 * time.Minute,
		Now: func() time.Time { return now }, NewToken: func() string { return "stage-token" },
	})

	if err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	current, _ := repos.Task.FindByID(task.ID)
	if current.Status != model.TaskStatusQueued || current.Stage != model.TaskStageUploaded {
		t.Fatalf("task after redispatch = status:%d stage:%s, want queued/uploaded (stage must not advance at dispatch)", current.Status, current.Stage)
	}
	job, _ := repos.TaskJob.FindByTaskAndType(task.ID, TaskJobTranscribe)
	if job == nil || job.Status != model.TaskStatusQueued || job.Stage != model.TaskStageTranscribing {
		t.Fatalf("job after redispatch = %+v, want queued with transcribing stage", job)
	}
}
