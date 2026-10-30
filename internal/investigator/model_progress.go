package investigator

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"vidlens/internal/domain"
)

type modelProgressKey struct{}

func questionCallTimeLimit() *domain.Problem {
	return &domain.Problem{Status: 504, Code: "MODEL_CALL_BUDGET_EXHAUSTED", Detail: "本次模型调用已达到等待时限，尚未取得完整结果。可以重新回答。", Meta: map[string]any{"can_retry": true}}
}

// Send/first-activity receipts are immediate; later receipts are at most one
// every five seconds, including when response and reasoning states alternate.
// Thousands of reasoning chunks must not become thousands of database writes.
type modelProgressEmitter struct {
	lastState string
	lastAt    time.Time
	startedAt time.Time
	limit     time.Duration
	emit      func(*domain.ModelProgress)
}

func (p *modelProgressEmitter) observe(state string, now time.Time) {
	if p.lastState != "" && p.lastState != "waiting" && now.Sub(p.lastAt) < 5*time.Second {
		return
	}
	p.lastState, p.lastAt = state, now
	p.emit(&domain.ModelProgress{State: state, ObservedAt: now.UTC(), ElapsedMS: max(int64(0), now.Sub(p.startedAt).Milliseconds()), CallLimitMS: p.limit.Milliseconds()})
}

func (s *session) progressEmitter(ctx context.Context, step *domain.Step, limits modelResponseLimits) *modelProgressEmitter {
	limit := limits.Total
	if deadline, ok := ctx.Deadline(); ok {
		limit = min(limit, max(time.Duration(0), time.Until(deadline)))
	}
	return &modelProgressEmitter{startedAt: time.Now(), limit: limit, emit: func(progress *domain.ModelProgress) {
		step.ModelProgress = progress
		cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if next, err := s.engine.Store.Step(cctx, s.claim, *step); err == nil {
			*step = next
		} else if ctx.Err() == nil && !errors.Is(err, domain.ErrFenced) {
			slog.Warn("model activity receipt unavailable")
		}
	}}
}
