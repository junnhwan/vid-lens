package ai

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"vid-lens/internal/model"
	"vid-lens/internal/observability"
	"vid-lens/internal/pkg/quota"
)

type Call struct {
	Operation, Provider, Model, Subject string
	InputChars                          int
	ASRSeconds                          float64
}
type Admission interface {
	Admit(context.Context, Call) (quota.Decision, error)
}
type UsageReservation struct {
	Key           string
	ReservedUnits float64
	Unit          string
}
type CallResult struct{ Err error }
type UsageController interface {
	Reserve(context.Context, Call) (UsageReservation, error)
	Settle(context.Context, UsageReservation, CallResult) error
	Release(context.Context, UsageReservation, CallResult) error
}

var ErrAdmissionRejected = errors.New("provider admission rejected")

type AdmissionError struct{ Decision quota.Decision }

func (e *AdmissionError) Error() string {
	return fmt.Sprintf("%v: scope=%s retry_after=%s", ErrAdmissionRejected, e.Decision.Scope, e.Decision.RetryAfter)
}
func (e *AdmissionError) Unwrap() error { return ErrAdmissionRejected }
func checkAdmission(ctx context.Context, a Admission, c Call) error {
	if a == nil {
		return nil
	}
	d, e := a.Admit(ctx, c)
	if e != nil {
		return e
	}
	if !d.Allowed {
		return &AdmissionError{d}
	}
	return nil
}

func beginAdmission(ctx context.Context, a Admission, call Call) (func(error), error) {
	if err := checkAdmission(ctx, a, call); err != nil {
		return nil, err
	}
	q, ok := a.(*QuotaAdmission)
	if !ok || q.Usage == nil {
		return func(error) {}, nil
	}
	reservation, err := q.Usage.Reserve(ctx, call)
	if err != nil {
		return nil, err
	}
	return func(callErr error) {
		result := CallResult{Err: callErr}
		if callErr != nil {
			if err := q.Usage.Release(ctx, reservation, result); err != nil {
				observability.Log(ctx, slog.Default(), slog.LevelError, "AI usage release failed",
					slog.String("operation", call.Operation), slog.String("error", observability.SafeError(err)))
			}
			return
		}
		if err := q.Usage.Settle(ctx, reservation, result); err != nil {
			observability.Log(ctx, slog.Default(), slog.LevelError, "AI usage settlement failed",
				slog.String("operation", call.Operation), slog.String("error", observability.SafeError(err)))
		}
	}, nil
}

type admittedChat struct {
	base ChatClient
	a    Admission
	p, m string
}

func AdmitChat(b ChatClient, a Admission, p, m string) ChatClient { return &admittedChat{b, a, p, m} }
func (x *admittedChat) Chat(c context.Context, m []ChatMessage) (answer string, err error) {
	finish, err := beginAdmission(c, x.a, Call{Operation: "chat", Provider: x.p, Model: x.m, InputChars: countChatMessageChars(m)})
	if err != nil {
		return "", err
	}
	defer func() { finish(err) }()
	return x.base.Chat(c, m)
}
func (x *admittedChat) StreamChat(c context.Context, m []ChatMessage, emit func(string) error) (err error) {
	finish, err := beginAdmission(c, x.a, Call{Operation: "chat_stream", Provider: x.p, Model: x.m, InputChars: countChatMessageChars(m)})
	if err != nil {
		return err
	}
	defer func() { finish(err) }()
	streaming, ok := x.base.(StreamingChatClient)
	if !ok {
		return errors.New("base chat client does not support streaming")
	}
	return streaming.StreamChat(c, m, emit)
}

type admittedEmbedding struct {
	base EmbeddingClient
	a    Admission
	p, m string
}

func AdmitEmbedding(b EmbeddingClient, a Admission, p, m string) EmbeddingClient {
	return &admittedEmbedding{b, a, p, m}
}
func (x *admittedEmbedding) Embed(c context.Context, s string) (vector []float32, err error) {
	finish, err := beginAdmission(c, x.a, Call{Operation: "embedding", Provider: x.p, Model: x.m, InputChars: utf8.RuneCountInString(s)})
	if err != nil {
		return nil, err
	}
	defer func() { finish(err) }()
	return x.base.Embed(c, s)
}

type admittedStrategy struct {
	base      Strategy
	a         Admission
	p, am, lm string
}

func AdmitStrategy(b Strategy, a Admission, p, am, lm string) Strategy {
	return &admittedStrategy{b, a, p, am, lm}
}
func (x *admittedStrategy) Transcribe(c context.Context, s string) (text string, err error) {
	finish, err := beginAdmission(c, x.a, Call{Operation: "asr", Provider: x.p, Model: x.am})
	if err != nil {
		return "", err
	}
	defer func() { finish(err) }()
	return x.base.Transcribe(c, s)
}
func (x *admittedStrategy) TranscribeChunks(c context.Context, paths []string) (string, error) {
	parts := make([]string, 0, len(paths))
	for _, path := range paths {
		text, err := x.Transcribe(c, path)
		if err != nil {
			return "", err
		}
		if text = strings.TrimSpace(text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}
func (x *admittedStrategy) Summarize(c context.Context, s string) (summary string, err error) {
	finish, err := beginAdmission(c, x.a, Call{Operation: "chat", Provider: x.p, Model: x.lm, InputChars: utf8.RuneCountInString(s)})
	if err != nil {
		return "", err
	}
	defer func() { finish(err) }()
	return x.base.Summarize(c, s)
}

type QuotaAdmission struct {
	Limiter       *quota.Limiter
	Attempts      AttemptBudget
	Usage         UsageController
	Now           func() time.Time
	NewAttemptKey func() string
	User          BucketRule
	Operation     BucketRule
	Provider      BucketRule
	Model         BucketRule
}
type BucketRule struct{ Capacity, Rate, Cost float64 }

func (q *QuotaAdmission) Admit(ctx context.Context, c Call) (quota.Decision, error) {
	metadata := GovernanceContextFromContext(ctx)
	if c.Subject == "" {
		c.Subject = metadata.Subject
	}
	// The shared budget limits extra retry work, not the normal first call of
	// each logical operation. A retry decorator sets AttemptKey only when it is
	// about to issue an additional provider request. This prevents a long ASR
	// task's legitimate chunks from exhausting a task-level retry allowance.
	if q.Attempts != nil && metadata.RetryBudgetID != "" && metadata.AttemptKey != "" {
		now := time.Now()
		if q.Now != nil {
			now = q.Now()
		}
		attemptKey := metadata.AttemptKey
		decision, err := q.Attempts.Consume(metadata.RetryBudgetID, attemptKey, model.RetryAttemptLayerProvider, now)
		if err != nil {
			return quota.Decision{}, err
		}
		if !decision.Allowed {
			return quota.Decision{}, &RetryBudgetError{Decision: decision}
		}
	}
	subject := c.Subject
	if subject == "" {
		subject = "system"
	}
	if q.Limiter == nil {
		return quota.Decision{Allowed: true}, nil
	}
	b := []quota.Bucket{}
	add := func(scope, key string, r BucketRule) {
		if r.Capacity > 0 {
			cost := r.Cost
			if cost <= 0 {
				cost = 1
			}
			b = append(b, quota.Bucket{Scope: scope, Key: key, Capacity: r.Capacity, Rate: r.Rate, Cost: cost})
		}
	}
	add("user", subject, q.User)
	add("operation", c.Operation, q.Operation)
	add("provider", c.Provider, q.Provider)
	add("model", c.Provider+":"+c.Model, q.Model)
	d, err := q.Limiter.AcquireForOperation(ctx, c.Operation, b)
	if err != nil && d.Allowed {
		return d, nil
	}
	return d, err
}
