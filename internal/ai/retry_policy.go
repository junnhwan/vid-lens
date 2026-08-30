package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"vid-lens/internal/model"
)

type AttemptDecision = model.RetryBudgetDecision

type AttemptBudget interface {
	Consume(budgetID, attemptKey, layer string, now time.Time) (model.RetryBudgetDecision, error)
}

var ErrRetryBudgetExhausted = errors.New("AI retry budget exhausted")

type RetryBudgetError struct{ Decision model.RetryBudgetDecision }

func (e *RetryBudgetError) Error() string {
	return fmt.Sprintf("%v: reason=%s attempts=%d/%d", ErrRetryBudgetExhausted, e.Decision.Reason, e.Decision.AttemptCount, e.Decision.MaxAttempts)
}
func (e *RetryBudgetError) Unwrap() error { return ErrRetryBudgetExhausted }

type governanceContextKey struct{}
type GovernanceContext struct{ RetryBudgetID, OperationKey, AttemptKey, Subject string }

type providerAttemptTimingKey struct{}

func withProviderAttemptTiming(ctx context.Context, observe func(time.Duration)) context.Context {
	return context.WithValue(ctx, providerAttemptTimingKey{}, observe)
}

func observeProviderAttemptTiming(ctx context.Context, duration time.Duration) {
	if ctx == nil {
		return
	}
	observe, _ := ctx.Value(providerAttemptTimingKey{}).(func(time.Duration))
	if observe != nil {
		observe(duration)
	}
}

func WithGovernanceContext(ctx context.Context, value GovernanceContext) context.Context {
	return context.WithValue(ctx, governanceContextKey{}, value)
}
func GovernanceContextFromContext(ctx context.Context) GovernanceContext {
	if ctx == nil {
		return GovernanceContext{}
	}
	v, _ := ctx.Value(governanceContextKey{}).(GovernanceContext)
	return v
}

func ShouldRetry(err error) bool {
	if err == nil {
		return false
	}
	var p *ProviderError
	if errors.As(err, &p) {
		return p.Retryable
	}
	var admission *AdmissionError
	if errors.As(err, &admission) {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded)
}
func RetryDelay(err error, attempt int, backoffs []time.Duration) time.Duration {
	if !ShouldRetry(err) {
		return 0
	}
	var p *ProviderError
	if errors.As(err, &p) && p.RetryAfter > 0 {
		return p.RetryAfter
	}
	var admission *AdmissionError
	if errors.As(err, &admission) && admission.Decision.RetryAfter > 0 {
		return admission.Decision.RetryAfter
	}
	if len(backoffs) == 0 {
		return 0
	}
	if attempt < 1 {
		attempt = 1
	}
	i := attempt - 1
	if i >= len(backoffs) {
		i = len(backoffs) - 1
	}
	return backoffs[i]
}

// ProviderRetryPolicy bounds only the additional calls issued inside one
// provider operation. Task-level retries remain owned by RetryScheduler and
// both layers consume the same durable RetryBudgetID when one is present.
type ProviderRetryPolicy struct {
	MaxRetries      int
	Backoffs        []time.Duration
	Sleep           func(context.Context, time.Duration) error
	NewOperationKey func() string
	BeginAttempt    func(int)
	ObserveAttempt  func(ProviderAttemptObservation)
}

// ProviderAttemptObservation exposes provider request and retry-wait latency
// without coupling the AI package to a metrics backend. Attempt is zero-based;
// RetryDelay is non-zero only when another request will be issued.
type ProviderAttemptObservation struct {
	Phase         string
	Attempt       int
	Duration      time.Duration
	Err           error
	RetryDelay    time.Duration
	SleepDuration time.Duration
}

func (p ProviderRetryPolicy) sleep(ctx context.Context, delay time.Duration) error {
	if p.Sleep != nil {
		return p.Sleep(ctx, delay)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (p ProviderRetryPolicy) operationKey(ctx context.Context) string {
	if key := GovernanceContextFromContext(ctx).OperationKey; key != "" {
		return key
	}
	if p.NewOperationKey != nil {
		if key := p.NewOperationKey(); key != "" {
			return key
		}
	}
	return uuid.NewString()
}

func providerAttemptKey(operationKey string, retry int) string {
	suffix := fmt.Sprintf(":provider-retry:%d", retry)
	if len(operationKey)+len(suffix) <= 128 {
		return operationKey + suffix
	}
	sum := sha256.Sum256([]byte(operationKey))
	return "op-" + hex.EncodeToString(sum[:16]) + suffix
}

func providerRetryContext(ctx context.Context, operationKey string, retry int) context.Context {
	metadata := GovernanceContextFromContext(ctx)
	metadata.OperationKey = operationKey
	if retry > 0 {
		metadata.AttemptKey = providerAttemptKey(operationKey, retry)
	} else {
		metadata.AttemptKey = ""
	}
	return WithGovernanceContext(ctx, metadata)
}

type retryingChat struct {
	base   ChatClient
	policy ProviderRetryPolicy
}

// RetryChat retries only errors explicitly classified as retryable by the
// provider adapter. Streaming calls are passed through without retry because a
// retry after partial emission would duplicate output.
func RetryChat(base ChatClient, policy ProviderRetryPolicy) ChatClient {
	return &retryingChat{base: base, policy: policy}
}

func (r *retryingChat) Chat(ctx context.Context, messages []ChatMessage) (string, error) {
	operationKey := r.policy.operationKey(ctx)
	for retry := 0; ; retry++ {
		answer, err := r.base.Chat(providerRetryContext(ctx, operationKey, retry), messages)
		if err == nil || retry >= r.policy.MaxRetries || !ShouldRetry(err) {
			return answer, err
		}
		if err := r.policy.sleep(ctx, RetryDelay(err, retry+1, r.policy.Backoffs)); err != nil {
			return "", err
		}
	}
}

func (r *retryingChat) StreamChat(ctx context.Context, messages []ChatMessage, emit func(string) error) error {
	streaming, ok := r.base.(StreamingChatClient)
	if !ok {
		return errors.New("base chat client does not support streaming")
	}
	operationKey := r.policy.operationKey(ctx)
	return streaming.StreamChat(providerRetryContext(ctx, operationKey, 0), messages, emit)
}

type retryingStrategy struct {
	base   Strategy
	policy ProviderRetryPolicy
}

// RetryStrategy retries only ASR calls classified as transient. Summarization
// is passed through because it has its own call semantics, while
// TranscribeChunks delegates to the retried single-chunk operation.
func RetryStrategy(base Strategy, policy ProviderRetryPolicy) Strategy {
	if base == nil {
		return nil
	}
	return &retryingStrategy{base: base, policy: policy}
}

func (r *retryingStrategy) Transcribe(ctx context.Context, audioPath string) (string, error) {
	operationKey := r.policy.operationKey(ctx)
	for retry := 0; ; retry++ {
		if r.policy.BeginAttempt != nil {
			r.policy.BeginAttempt(retry)
		}
		startedAt := time.Now()
		measuredDuration := time.Duration(0)
		measured := false
		attemptCtx := withProviderAttemptTiming(providerRetryContext(ctx, operationKey, retry), func(duration time.Duration) {
			measuredDuration, measured = duration, true
		})
		text, err := r.base.Transcribe(attemptCtx, audioPath)
		duration := time.Since(startedAt)
		if measured {
			duration = measuredDuration
		}
		willRetry := err != nil && retry < r.policy.MaxRetries && ShouldRetry(err)
		delay := time.Duration(0)
		if willRetry {
			delay = RetryDelay(err, retry+1, r.policy.Backoffs)
		}
		observation := ProviderAttemptObservation{Phase: "request", Attempt: retry, Duration: duration, Err: err, RetryDelay: delay}
		if r.policy.ObserveAttempt != nil {
			r.policy.ObserveAttempt(observation)
		}
		if !willRetry {
			return text, err
		}
		sleepStartedAt := time.Now()
		if err := r.policy.sleep(ctx, delay); err != nil {
			if r.policy.ObserveAttempt != nil {
				observation.Phase = "retry_wait"
				observation.Duration = 0
				observation.SleepDuration = time.Since(sleepStartedAt)
				r.policy.ObserveAttempt(observation)
			}
			return "", err
		}
		if r.policy.ObserveAttempt != nil {
			observation.Phase = "retry_wait"
			observation.Duration = 0
			observation.SleepDuration = time.Since(sleepStartedAt)
			r.policy.ObserveAttempt(observation)
		}
	}
}

func (r *retryingStrategy) TranscribeChunks(ctx context.Context, audioPaths []string) (string, error) {
	if len(audioPaths) == 0 {
		return "", fmt.Errorf("没有可转写的音频片段")
	}
	parts := make([]string, 0, len(audioPaths))
	for i, path := range audioPaths {
		text, err := r.Transcribe(ctx, path)
		if err != nil {
			return "", fmt.Errorf("第 %d 段 ASR 失败: %w", i+1, err)
		}
		if text = strings.TrimSpace(text); text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("ASR 返回空结果")
	}
	return strings.Join(parts, "\n\n"), nil
}

func (r *retryingStrategy) Summarize(ctx context.Context, text string) (string, error) {
	return r.base.Summarize(ctx, text)
}
