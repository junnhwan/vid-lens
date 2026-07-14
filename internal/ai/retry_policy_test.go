package ai

import (
	"context"
	"errors"
	"testing"
	"time"
)

type sequenceChat struct {
	errors []error
	calls  int
}

func (s *sequenceChat) Chat(context.Context, []ChatMessage) (string, error) {
	s.calls++
	if s.calls <= len(s.errors) && s.errors[s.calls-1] != nil {
		return "", s.errors[s.calls-1]
	}
	return "ok", nil
}

func TestRetryChatRetriesTwoProviderFailuresThenSucceeds(t *testing.T) {
	provider := &sequenceChat{errors: []error{
		&ProviderError{Class: ErrorProvider5xx, StatusCode: 503, Retryable: true},
		&ProviderError{Class: ErrorProvider5xx, StatusCode: 503, Retryable: true},
	}}
	var delays []time.Duration
	client := RetryChat(provider, ProviderRetryPolicy{
		MaxRetries: 2,
		Backoffs:   []time.Duration{time.Second, 2 * time.Second},
		Sleep: func(_ context.Context, delay time.Duration) error {
			delays = append(delays, delay)
			return nil
		},
		NewOperationKey: func() string { return "op-generated" },
	})

	answer, err := client.Chat(context.Background(), nil)
	if err != nil || answer != "ok" {
		t.Fatalf("answer=%q err=%v", answer, err)
	}
	if provider.calls != 3 {
		t.Fatalf("provider calls=%d want=3", provider.calls)
	}
	if len(delays) != 2 || delays[0] != time.Second || delays[1] != 2*time.Second {
		t.Fatalf("delays=%v", delays)
	}
}

func TestRetryChatDoesNotRetryNonRetryableProviderError(t *testing.T) {
	providerErr := &ProviderError{Class: ErrorInvalidRequest, StatusCode: 400, Retryable: false}
	provider := &sequenceChat{errors: []error{providerErr}}
	client := RetryChat(provider, ProviderRetryPolicy{MaxRetries: 2, Sleep: func(context.Context, time.Duration) error {
		t.Fatal("non-retryable error must not sleep")
		return nil
	}})

	_, err := client.Chat(context.Background(), nil)
	if !errors.Is(err, providerErr) || provider.calls != 1 {
		t.Fatalf("err=%v provider calls=%d", err, provider.calls)
	}
}

func TestRetryChatHonorsRetryAfterAndStopsOnContextCancel(t *testing.T) {
	providerErr := &ProviderError{Class: ErrorRateLimited, StatusCode: 429, Retryable: true, RetryAfter: 7 * time.Second}
	provider := &sequenceChat{errors: []error{providerErr}}
	ctx, cancel := context.WithCancel(context.Background())
	client := RetryChat(provider, ProviderRetryPolicy{
		MaxRetries: 2,
		Backoffs:   []time.Duration{time.Second},
		Sleep: func(_ context.Context, delay time.Duration) error {
			if delay != 7*time.Second {
				t.Fatalf("delay=%v want=7s", delay)
			}
			cancel()
			return ctx.Err()
		},
	})

	_, err := client.Chat(ctx, nil)
	if !errors.Is(err, context.Canceled) || provider.calls != 1 {
		t.Fatalf("err=%v provider calls=%d", err, provider.calls)
	}
}

type governanceCapturingChat struct {
	contexts []GovernanceContext
	calls    int
}

func (c *governanceCapturingChat) Chat(ctx context.Context, _ []ChatMessage) (string, error) {
	c.contexts = append(c.contexts, GovernanceContextFromContext(ctx))
	c.calls++
	if c.calls == 1 {
		return "", &ProviderError{Class: ErrorProvider5xx, StatusCode: 503, Retryable: true}
	}
	return "ok", nil
}

func TestRetryChatUsesStableOperationKeyForProviderAttempt(t *testing.T) {
	provider := &governanceCapturingChat{}
	client := RetryChat(provider, ProviderRetryPolicy{MaxRetries: 1, Sleep: func(context.Context, time.Duration) error { return nil }})
	ctx := WithGovernanceContext(context.Background(), GovernanceContext{
		RetryBudgetID: "budget-1",
		OperationKey:  "task-9-job-3-asr-chunk-2",
		Subject:       "user:7",
	})

	if _, err := client.Chat(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if len(provider.contexts) != 2 {
		t.Fatalf("contexts=%d", len(provider.contexts))
	}
	if provider.contexts[0].AttemptKey != "" || provider.contexts[1].AttemptKey != "task-9-job-3-asr-chunk-2:provider-retry:1" {
		t.Fatalf("governance contexts=%+v", provider.contexts)
	}
	if provider.contexts[1].RetryBudgetID != "budget-1" || provider.contexts[1].Subject != "user:7" {
		t.Fatalf("retry lost governance metadata: %+v", provider.contexts[1])
	}
}
