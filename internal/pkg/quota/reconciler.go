package quota

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"vid-lens/internal/model"
)

type CompensationStore interface {
	ListDueCompensations(time.Time, int) ([]model.QuotaCompensation, error)
	ClaimCompensation(id int64, token string, now, until time.Time) (bool, error)
	CompleteCompensation(id int64, token string, now time.Time) error
	RetryCompensation(id int64, token, message string, next time.Time, maxAttempts int) error
}
type UsageCache interface {
	Apply(context.Context, model.QuotaCompensation) error
}

type ReconcilerConfig struct {
	BatchSize, MaxAttempts int
	Backoff, Lease         time.Duration
	Now                    func() time.Time
	NewToken               func() string
}
type Reconciler struct {
	store  CompensationStore
	cache  UsageCache
	config ReconcilerConfig
}

func NewReconciler(s CompensationStore, c UsageCache, cfg ReconcilerConfig) *Reconciler {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 8
	}
	if cfg.Backoff <= 0 {
		cfg.Backoff = time.Minute
	}
	if cfg.Lease <= 0 {
		cfg.Lease = time.Minute
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.NewToken == nil {
		cfg.NewToken = uuid.NewString
	}
	return &Reconciler{store: s, cache: c, config: cfg}
}

// Start runs the durable compensation loop until ctx is canceled. The returned
// channel closes only after the worker exits, which gives tests and graceful
// shutdown code an explicit lifecycle signal.
func (r *Reconciler) Start(ctx context.Context, interval time.Duration) <-chan struct{} {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if err := r.RunOnce(ctx); err != nil && ctx.Err() == nil {
				slog.ErrorContext(ctx, "quota reconciliation failed", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return done
}

func (r *Reconciler) RunOnce(ctx context.Context) error {
	if r == nil || r.store == nil || r.cache == nil {
		return fmt.Errorf("quota reconciler is not initialized")
	}
	now := r.config.Now()
	events, err := r.store.ListDueCompensations(now, r.config.BatchSize)
	if err != nil {
		return err
	}
	for _, event := range events {
		if err := ctx.Err(); err != nil {
			return err
		}
		token := r.config.NewToken()
		claimed, err := r.store.ClaimCompensation(event.ID, token, now, now.Add(r.config.Lease))
		if err != nil {
			return err
		}
		if !claimed {
			continue
		}
		if err := r.cache.Apply(ctx, event); err != nil {
			next := now.Add(r.config.Backoff * time.Duration(1<<minInt(event.AttemptCount, 6)))
			if retryErr := r.store.RetryCompensation(event.ID, token, err.Error(), next, r.config.MaxAttempts); retryErr != nil {
				return retryErr
			}
			continue
		}
		if err := r.store.CompleteCompensation(event.ID, token, now); err != nil {
			return err
		}
	}
	return nil
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
