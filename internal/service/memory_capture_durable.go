package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

type DurableMemoryCapture struct {
	repo      *repository.MemoryRepository
	extractor MemoryExtractor
	writer    *AsyncMemoryWriter
	cancel    context.CancelFunc
	done      chan struct{}
	closeOnce sync.Once
}

func NewDurableMemoryCapture(repo *repository.MemoryRepository, extractor MemoryExtractor, writer *AsyncMemoryWriter) *DurableMemoryCapture {
	ctx, cancel := context.WithCancel(context.Background())
	c := &DurableMemoryCapture{repo: repo, extractor: extractor, writer: writer, cancel: cancel, done: make(chan struct{})}
	go c.run(ctx)
	return c
}

func (c *DurableMemoryCapture) process(ctx context.Context) (bool, error) {
	job, err := c.repo.ClaimMemoryCapture(ctx, time.Now().UTC())
	if err != nil || job == nil {
		return false, err
	}
	jobCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	message, err := c.repo.MemoryCaptureMessage(jobCtx, *job)
	if job.ExtractorVersion != model.MemoryExtractorVersion {
		err = fmt.Errorf("unsupported memory extractor version: %s", job.ExtractorVersion)
	}
	if err == nil && message != nil {
		inputs, policyErr := c.repo.ResolveMemoryPolicyInputs(jobCtx, job.UserID, job.SessionID)
		err = policyErr
		allowed := inputs.SessionPolicy == "enabled" || (inputs.SessionPolicy == "inherit" && inputs.UserEnabled)
		if err == nil && allowed {
			var candidates []MemoryCandidate
			candidates, err = c.extractor.Extract(jobCtx, MemoryExtractionRequest{UserID: job.UserID, SessionID: job.SessionID, UserText: message.Content, SourceRef: fmt.Sprintf("chat_message:%d", message.ID)})
			if err == nil {
				for _, candidate := range candidates {
					if err = validateMemoryCandidate(candidate); err != nil {
						break
					}
					if err = c.writer.write(jobCtx, candidate); err != nil {
						break
					}
				}
			}
		}
	}
	if ctx.Err() != nil {
		return true, ctx.Err()
	} // Leave the lease for recovery.
	var projectionError *MemoryProjectionError
	job.ProjectionPending = errors.As(err, &projectionError) || job.ProjectionPending
	if finishErr := c.repo.FinishMemoryCapture(ctx, *job, err == nil); finishErr != nil {
		return true, finishErr
	}
	if err != nil {
		observeMemoryBackground("durable_capture", "retry")
	} else {
		observeMemoryBackground("durable_capture", "completed")
	}
	return true, err
}

func (c *DurableMemoryCapture) run(ctx context.Context) {
	defer close(c.done)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for i := 0; i < 16; i++ {
				worked, err := c.process(ctx)
				if err != nil || !worked {
					break
				}
			}
		}
	}
}

func (c *DurableMemoryCapture) Close(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(c.cancel)
	select {
	case <-c.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
