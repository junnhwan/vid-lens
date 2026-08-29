package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/google/uuid"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/observability"
	"vid-lens/internal/pkg/quota"
	"vid-lens/internal/repository"
)

// AIUsageGovernor performs the durable reserve-before-call protocol. PostgreSQL
// is authoritative; Redis is an idempotent daily cache repaired by the outbox.
type AIUsageGovernor struct {
	repos    *repository.Repositories
	cache    quota.UsageCache
	location *time.Location
	now      func() time.Time
	newKey   func() string
}

func NewAIUsageGovernor(repos *repository.Repositories, cache quota.UsageCache, location *time.Location) *AIUsageGovernor {
	if location == nil {
		location = time.UTC
	}
	return &AIUsageGovernor{repos: repos, cache: cache, location: location, now: time.Now, newKey: uuid.NewString}
}
func (g *AIUsageGovernor) Reserve(ctx context.Context, call ai.Call) (ai.UsageReservation, error) {
	if g == nil || g.repos == nil || g.repos.UsageLedger == nil {
		return ai.UsageReservation{}, fmt.Errorf("AI usage ledger is not initialized")
	}
	corr := observability.CorrelationFromContext(ctx)
	if corr.UserID <= 0 {
		return ai.UsageReservation{}, nil
	}
	now := g.now()
	unit, units, kind := usageReservationForCall(call)
	key := g.newKey()
	ledger, _, event, err := g.repos.UsageLedger.Reserve(repository.UsageReservation{IdempotencyKey: key, UserID: corr.UserID, TaskID: corr.TaskID, JobID: corr.JobID, Kind: kind, Operation: call.Operation, Provider: call.Provider, Model: call.Model, Unit: unit, ReservedUnits: units, UsageDate: now.In(g.location).Format("2006-01-02"), ExpiresAt: now.Add(24 * time.Hour), Now: now})
	if err != nil {
		return ai.UsageReservation{}, err
	}
	if event != nil {
		g.applyCompensation(ctx, *event)
	}
	return ai.UsageReservation{Key: ledger.IdempotencyKey, ReservedUnits: ledger.ReservedUnits, Unit: ledger.Unit}, nil
}
func usageReservationForCall(call ai.Call) (unit string, units float64, kind string) {
	switch call.Operation {
	case "asr":
		kind = model.AICallKindASR
		if call.ASRSeconds > 0 {
			return model.UsageUnitSecond, call.ASRSeconds, kind
		}
		return model.UsageUnitCall, 1, kind
	case "embedding":
		kind = model.AICallKindEmbedding
		return model.UsageUnitToken, math.Max(1, math.Ceil(float64(call.InputChars)/4)), kind
	default:
		kind = model.AICallKindLLM
		return model.UsageUnitToken, math.Max(1, math.Ceil(float64(call.InputChars)/4)), kind
	}
}
func (g *AIUsageGovernor) Settle(ctx context.Context, res ai.UsageReservation, _ ai.CallResult) error {
	if res.Key == "" {
		return nil
	}
	now := g.now()
	actual := res.ReservedUnits
	source := model.UsageSourceEstimated
	if res.Unit == model.UsageUnitCall || res.Unit == model.UsageUnitSecond {
		source = model.UsageSourceActual
	}
	_, _, event, err := g.repos.UsageLedger.Settle(res.Key, repository.UsageSettlement{ActualUnits: &actual, UsageSource: source, Now: now})
	if err != nil {
		return err
	}
	if event != nil {
		g.applyCompensation(ctx, *event)
	}
	return nil
}
func (g *AIUsageGovernor) Release(ctx context.Context, res ai.UsageReservation, result ai.CallResult) error {
	if res.Key == "" {
		return nil
	}
	reason := "provider call failed"
	if result.Err != nil {
		reason = observability.SafeError(result.Err)
	}
	_, _, event, err := g.repos.UsageLedger.Release(res.Key, reason, g.now())
	if err != nil {
		return err
	}
	if event != nil {
		g.applyCompensation(ctx, *event)
	}
	return nil
}
func (g *AIUsageGovernor) applyCompensation(ctx context.Context, event model.QuotaCompensation) {
	if g.cache == nil || g.repos.QuotaCompensation == nil {
		return
	}
	now := g.now()
	token := uuid.NewString()
	claimed, err := g.repos.QuotaCompensation.ClaimCompensation(event.ID, token, now, now.Add(time.Minute))
	if err != nil || !claimed {
		slog.WarnContext(ctx, "quota compensation claim failed", slog.Int64("event_id", event.ID), slog.String("error", observability.SafeError(err)))
		return
	}
	if err := g.cache.Apply(ctx, event); err != nil {
		_ = g.repos.QuotaCompensation.RetryCompensation(event.ID, token, observability.SafeError(err), now.Add(time.Minute), 8)
		slog.WarnContext(ctx, "quota cache update deferred", slog.Int64("event_id", event.ID), slog.String("error", observability.SafeError(err)))
		return
	}
	if err := g.repos.QuotaCompensation.CompleteCompensation(event.ID, token, now); err != nil {
		slog.WarnContext(ctx, "quota compensation completion deferred", slog.Int64("event_id", event.ID), slog.String("error", observability.SafeError(err)))
	}
}
