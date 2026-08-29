package ai

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/quota"
)

type memoryAttemptBudget struct {
	mu     sync.Mutex
	max, n int
	keys   map[string]bool
}

func (b *memoryAttemptBudget) Consume(_ string, key, layer string, _ time.Time) (AttemptDecision, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.keys[key] {
		return AttemptDecision{Allowed: true, Duplicate: true, AttemptCount: b.n, MaxAttempts: b.max}, nil
	}
	if b.n >= b.max {
		return AttemptDecision{Allowed: false, Reason: model.RetryBudgetReasonExhausted, AttemptCount: b.n, MaxAttempts: b.max}, nil
	}
	b.keys[key] = true
	b.n++
	return AttemptDecision{Allowed: true, AttemptCount: b.n, MaxAttempts: b.max}, nil
}

type countingMockProvider struct {
	mu       sync.Mutex
	calls    int
	failures int
}

func (p *countingMockProvider) Chat(context.Context, []ChatMessage) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.failures > 0 {
		p.failures--
		return "", &ProviderError{Class: ErrorProvider5xx, Retryable: true, StatusCode: 503}
	}
	return "ok", nil
}

func TestProviderCallsAcrossConcurrentRetriesSharePersistentBudget(t *testing.T) {
	budget := &memoryAttemptBudget{max: 4, keys: map[string]bool{}}
	admission := &QuotaAdmission{Attempts: budget, Now: func() time.Time { return time.Unix(1, 0) }}
	provider := &countingMockProvider{failures: 100}
	client := AdmitChat(provider, admission, "mock", "chat")
	baseCtx := WithGovernanceContext(context.Background(), GovernanceContext{RetryBudgetID: "budget-1", Subject: "user-1"})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx := WithGovernanceContext(baseCtx, GovernanceContext{RetryBudgetID: "budget-1", Subject: "user-1", AttemptKey: fmt.Sprintf("provider-retry-%02d", i)})
			_, _ = client.Chat(ctx, nil)
		}(i)
	}
	wg.Wait()
	if provider.calls != 4 {
		t.Fatalf("provider calls=%d want=4", provider.calls)
	}
}

func TestInitialProviderCallsDoNotConsumeSharedRetryBudget(t *testing.T) {
	budget := &memoryAttemptBudget{max: 1, keys: map[string]bool{}}
	admission := &QuotaAdmission{Attempts: budget}
	provider := &countingMockProvider{}
	client := AdmitChat(provider, admission, "mock", "chat")
	ctx := WithGovernanceContext(context.Background(), GovernanceContext{RetryBudgetID: "budget-normal-calls", Subject: "user-1"})

	for i := 0; i < 3; i++ {
		if _, err := client.Chat(ctx, nil); err != nil {
			t.Fatalf("normal provider call %d: %v", i+1, err)
		}
	}
	if provider.calls != 3 {
		t.Fatalf("provider calls=%d want=3", provider.calls)
	}
	budget.mu.Lock()
	consumed := budget.n
	budget.mu.Unlock()
	if consumed != 0 {
		t.Fatalf("normal calls consumed retry permits=%d want=0", consumed)
	}
}

func TestNonRetryableProviderErrorDoesNotRequestAnotherAttempt(t *testing.T) {
	err := &ProviderError{Class: ErrorInvalidRequest, StatusCode: 400, Retryable: false}
	if ShouldRetry(err) {
		t.Fatal("400 must not be retried")
	}
	if d := RetryDelay(err, 3, []time.Duration{time.Second}); d != 0 {
		t.Fatalf("delay=%v", d)
	}
	limited := &ProviderError{Class: ErrorRateLimited, StatusCode: 429, Retryable: true, RetryAfter: 7 * time.Second}
	if !ShouldRetry(limited) || RetryDelay(limited, 1, []time.Duration{time.Second}) != 7*time.Second {
		t.Fatal("Retry-After must win")
	}
	if !errors.Is(&RetryBudgetError{Decision: AttemptDecision{Reason: model.RetryBudgetReasonExhausted}}, ErrRetryBudgetExhausted) {
		t.Fatal("budget error classification")
	}
}

var _ = quota.Decision{}
