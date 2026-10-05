package ai

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"vid-lens/internal/pkg/quota"
)

type queuedASRAdmission struct {
	remaining int
	delay     time.Duration
	calls     int
}

func (a *queuedASRAdmission) Admit(context.Context, Call) (quota.Decision, error) {
	a.calls++
	if a.remaining > 0 {
		a.remaining--
		return quota.Decision{Allowed: false, Scope: "model", RetryAfter: a.delay}, nil
	}
	return quota.Decision{Allowed: true}, nil
}

type countedASR struct {
	fakeStrategy
	calls int
}

func (s *countedASR) TranscribeDetailed(context.Context, string) (TranscriptionResult, error) {
	s.calls++
	return TranscriptionResult{Text: "原始语音内容。"}, nil
}

func TestQueuedASRAdmissionDoesNotBecomeProviderRetry(t *testing.T) {
	admission := &queuedASRAdmission{remaining: 4, delay: time.Millisecond}
	provider := &countedASR{}
	strategy := RetryStrategy(AdmitStrategy(provider, admission, "provider", "asr", "llm"), ProviderRetryPolicy{MaxRetries: 0})
	result, err := TranscribeDetailed(context.Background(), strategy, "audio")
	if err != nil || result.Text != "原始语音内容。" || provider.calls != 1 || admission.calls != 5 {
		t.Fatalf("result=%+v err=%v provider=%d admissions=%d", result, err, provider.calls, admission.calls)
	}
}

func TestQueuedASRAdmissionCancellationDoesNotCallProvider(t *testing.T) {
	admission := &queuedASRAdmission{remaining: 5, delay: time.Hour}
	provider := &countedASR{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, err := TranscribeDetailed(ctx, AdmitStrategy(provider, admission, "provider", "asr", "llm"), "audio")
	if !errors.Is(err, context.DeadlineExceeded) || provider.calls != 0 {
		t.Fatalf("err=%v provider calls=%d", err, provider.calls)
	}
}

func TestLocalQuotaDenialDoesNotConsumeProviderRetryPermit(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	budget := &memoryAttemptBudget{max: 1, keys: map[string]bool{}}
	admission := &QuotaAdmission{Limiter: quota.NewLimiter(client, quota.FailClosed), Attempts: budget, Model: BucketRule{Capacity: 1, Rate: 0, Cost: 1}}
	call := Call{Operation: "asr", Provider: "p", Model: "m"}
	if decision, err := admission.Admit(context.Background(), call); err != nil || !decision.Allowed {
		t.Fatalf("initial admission=%+v %v", decision, err)
	}
	ctx := WithGovernanceContext(context.Background(), GovernanceContext{RetryBudgetID: "job", AttemptKey: "provider-retry-1"})
	for i := 0; i < 5; i++ {
		if decision, err := admission.Admit(ctx, call); err != nil || decision.Allowed {
			t.Fatalf("denied admission=%+v %v", decision, err)
		}
	}
	if budget.n != 0 {
		t.Fatalf("local denials consumed %d provider retries", budget.n)
	}
	server.FlushAll()
	if decision, err := admission.Admit(ctx, call); err != nil || !decision.Allowed || budget.n != 1 {
		t.Fatalf("admitted retry=%+v %v permits=%d", decision, err, budget.n)
	}
}
