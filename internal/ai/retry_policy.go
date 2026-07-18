package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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
