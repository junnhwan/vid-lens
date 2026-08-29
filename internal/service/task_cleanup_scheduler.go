package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"vid-lens/internal/observability"
)

// TaskCleanupSchedulerConfig controls only how often durable cleanup intents
// are scanned. Claim leases and retry backoff remain owned by
// TaskCleanupConfig because they define execution semantics.
type TaskCleanupSchedulerConfig struct {
	Interval  time.Duration
	BatchSize int
	Now       func() time.Time
}

func (c TaskCleanupSchedulerConfig) normalized() TaskCleanupSchedulerConfig {
	if c.Interval <= 0 {
		c.Interval = 30 * time.Second
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 20
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return c
}

// TaskCleanupScheduler periodically retries durable task cleanup jobs. Job
// claims remain inside TaskCleanupService so immediate execution and scheduled
// execution share the same lease and idempotency rules.
type TaskCleanupScheduler struct {
	cleanup *TaskCleanupService
	config  TaskCleanupSchedulerConfig
	wg      sync.WaitGroup
}

func NewTaskCleanupScheduler(cleanup *TaskCleanupService, config TaskCleanupSchedulerConfig) *TaskCleanupScheduler {
	return &TaskCleanupScheduler{cleanup: cleanup, config: config.normalized()}
}

// Start launches one context-bound worker. It scans immediately so pending work
// does not have to wait for the first ticker interval.
func (s *TaskCleanupScheduler) Start(ctx context.Context) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.config.Interval)
		defer ticker.Stop()
		for {
			if err := s.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				observability.Log(ctx, slog.Default(), slog.LevelError, "task cleanup scheduler failed", slog.String("error", observability.SafeError(err)))
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// Wait blocks until the scheduler worker has observed context cancellation.
func (s *TaskCleanupScheduler) Wait() {
	s.wg.Wait()
}

// RunOnce executes every due job in the bounded batch. One failed cleanup does
// not starve later jobs; callers receive the joined batch errors for logging.
func (s *TaskCleanupScheduler) RunOnce(ctx context.Context) error {
	if s == nil || s.cleanup == nil || s.cleanup.repo == nil || s.cleanup.repo.TaskCleanup == nil {
		return fmt.Errorf("task cleanup scheduler is not initialized")
	}
	jobs, err := s.cleanup.repo.TaskCleanup.FindDue(s.config.Now(), s.config.BatchSize)
	if err != nil {
		return fmt.Errorf("find due task cleanup jobs: %w", err)
	}
	var batchErr error
	for _, job := range jobs {
		if err := ctx.Err(); err != nil {
			return errors.Join(batchErr, err)
		}
		if err := s.cleanup.ExecuteJob(ctx, job.ID); err != nil {
			batchErr = errors.Join(batchErr, fmt.Errorf("execute task cleanup job %d: %w", job.ID, err))
		}
	}
	return batchErr
}
