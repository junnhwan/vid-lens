package mq

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"vid-lens/internal/repository"
)

var ErrProcessingLeaseLost = errors.New("processing lease lost")

type processingLeaseOwnerKey struct{}

type processingLeaseOwner struct {
	repos   *repository.Repositories
	taskID  int64
	jobType string
	token   string
	now     func() time.Time
}

func withProcessingLeaseOwner(ctx context.Context, owner *processingLeaseOwner) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, processingLeaseOwnerKey{}, owner)
}

func processingLeaseOwnerFromContext(ctx context.Context) *processingLeaseOwner {
	if ctx == nil {
		return nil
	}
	owner, _ := ctx.Value(processingLeaseOwnerKey{}).(*processingLeaseOwner)
	return owner
}

// requireProcessingLease fences every remote call and mutable stage side effect.
// It cannot make a remote provider exactly-once, but it prevents an already
// fenced-out worker from starting the next call or overwriting newer results.
func requireProcessingLease(ctx context.Context) error {
	if err := context.Cause(ctx); err != nil {
		if errors.Is(err, ErrProcessingLeaseLost) {
			return ErrProcessingLeaseLost
		}
		return err
	}
	owner := processingLeaseOwnerFromContext(ctx)
	if owner == nil {
		return nil // helpers remain usable outside a Kafka processing lease.
	}
	if owner.repos == nil {
		return fmt.Errorf("%w: repositories unavailable", ErrProcessingLeaseLost)
	}
	now := time.Now()
	if owner.now != nil {
		now = owner.now()
	}
	owned, err := owner.repos.OwnsTaskProcessing(repository.TaskProcessingLeaseRequest{
		TaskID: owner.taskID, JobType: owner.jobType, Token: owner.token, Now: now,
	})
	if err != nil {
		return fmt.Errorf("verify processing lease: %w", err)
	}
	if !owned {
		return ErrProcessingLeaseLost
	}
	return nil
}

func (c *Consumer) startProcessingLeaseHeartbeat(parent context.Context, taskID int64, jobType, token string) (context.Context, func()) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancelCause(parent)
	owner := &processingLeaseOwner{repos: c.repo, taskID: taskID, jobType: jobType, token: token, now: c.currentTime}
	ctx = withProcessingLeaseOwner(ctx, owner)

	lease := c.processingLease
	if lease <= 0 {
		lease = 30 * time.Minute
	}
	interval := c.leaseHeartbeatInterval
	if interval <= 0 {
		interval = lease / 3
		if interval <= 0 {
			interval = time.Second
		}
	}

	ticker := time.NewTicker(interval)
	done := make(chan struct{})
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel(context.Canceled)
			<-done
		})
	}
	go func() {
		defer close(done)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				now := c.currentTime()
				renewed, err := c.repo.RenewTaskProcessing(repository.TaskProcessingLeaseRequest{
					TaskID: taskID, JobType: jobType, Token: token, Now: now, LeaseUntil: now.Add(lease),
				})
				if err != nil || !renewed {
					cancel(ErrProcessingLeaseLost)
					return
				}
			}
		}
	}()
	return ctx, stop
}

// runLeasedSideEffect executes a MySQL side effect in the same transaction as
// the task/job processing-lease check. Direct helper calls outside a Kafka
// processing context retain their existing behavior for HTTP paths and tests.
func (c *Consumer) runLeasedSideEffect(ctx context.Context, fn func(*repository.Repositories) error) error {
	if fn == nil {
		return nil
	}
	owner := processingLeaseOwnerFromContext(ctx)
	if owner == nil {
		if c == nil || c.repo == nil {
			return fmt.Errorf("repositories unavailable")
		}
		return fn(c.repo)
	}
	if owner.repos == nil {
		return ErrProcessingLeaseLost
	}
	now := time.Now()
	if owner.now != nil {
		now = owner.now()
	}
	owned, err := owner.repos.RunWithTaskProcessingLease(repository.TaskProcessingLeaseRequest{
		TaskID: owner.taskID, JobType: owner.jobType, Token: owner.token, Now: now,
	}, fn)
	if err != nil {
		return fmt.Errorf("run processing-lease side effect: %w", err)
	}
	if !owned {
		return ErrProcessingLeaseLost
	}
	return nil
}
