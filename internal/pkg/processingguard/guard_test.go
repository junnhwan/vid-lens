package processingguard

import (
	"context"
	"errors"
	"testing"
)

func TestCheckRunsGuardFromContext(t *testing.T) {
	want := errors.New("lease lost")
	calls := 0
	ctx := With(context.Background(), func(context.Context) error {
		calls++
		return want
	})
	if err := Check(ctx); !errors.Is(err, want) {
		t.Fatalf("Check() error = %v, want %v", err, want)
	}
	if calls != 1 {
		t.Fatalf("guard calls = %d, want 1", calls)
	}
}

func TestCheckPrefersContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cause := errors.New("heartbeat lost lease")
	cancel(cause)
	guardCalls := 0
	ctx = With(ctx, func(context.Context) error {
		guardCalls++
		return nil
	})
	if err := Check(ctx); !errors.Is(err, cause) {
		t.Fatalf("Check() error = %v, want %v", err, cause)
	}
	if guardCalls != 0 {
		t.Fatalf("guard calls = %d, want 0 for canceled context", guardCalls)
	}
}
