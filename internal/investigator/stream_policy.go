package investigator

import (
	"context"
	"time"
)

const streamProgressPolicy = "stream-progress-2026-10-24-v1"

// This belongs to the frozen run and each durable logical input. A deployment
// must not silently increase the deadline of an older, already submitted call.
type modelResponsePolicy struct {
	Revision string `json:"revision"`
	TotalMS  int64  `json:"total_ms"`
	FirstMS  int64  `json:"first_activity_ms"`
	IdleMS   int64  `json:"idle_ms"`
}

func articleResponsePolicy() *modelResponsePolicy {
	return &modelResponsePolicy{streamProgressPolicy, 600000, 300000, 300000}
}

func (p *modelResponsePolicy) valid() bool {
	return p != nil && *p == *articleResponsePolicy()
}

func (p *modelResponsePolicy) limits() modelResponseLimits {
	return modelResponseLimits{Total: time.Duration(p.TotalMS) * time.Millisecond, First: time.Duration(p.FirstMS) * time.Millisecond, Idle: time.Duration(p.IdleMS) * time.Millisecond, BudgetDeadline: true}
}

func (s *session) responsePolicy() *modelResponsePolicy {
	if s.articleLoop() && s.cp.ArticleLoop != nil {
		return s.cp.ArticleLoop.Budget.ResponsePolicy
	}
	return nil
}

type responsePolicyKey struct{}

func modelHTTPTimeout(ctx context.Context) time.Duration {
	if p, ok := ctx.Value(responsePolicyKey{}).(*modelResponsePolicy); ok && p.valid() {
		return time.Duration(p.TotalMS) * time.Millisecond
	}
	return 300 * time.Second
}
