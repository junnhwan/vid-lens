package ai

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "secret timeout payload" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestProviderErrorFromHTTP(t *testing.T) {
	tests := []struct {
		status int
		retry  bool
		class  ErrorClass
	}{
		{429, true, ErrorRateLimited}, {500, true, ErrorProvider5xx}, {503, true, ErrorProvider5xx}, {401, false, ErrorAuth}, {400, false, ErrorInvalidRequest},
	}
	for _, tt := range tests {
		h := http.Header{"X-Request-Id": []string{"req-1"}, "Retry-After": []string{"3"}}
		err := ProviderHTTPError("openai", "chat", tt.status, h, []byte(strings.Repeat("x", 1000)))
		var pe *ProviderError
		if !errors.As(err, &pe) {
			t.Fatalf("status %d not typed", tt.status)
		}
		if pe.Class != tt.class || pe.Retryable != tt.retry || pe.RequestID != "req-1" {
			t.Fatalf("unexpected: %+v", pe)
		}
		if tt.status == 429 && pe.RetryAfter != 3*time.Second {
			t.Fatalf("retry after=%v", pe.RetryAfter)
		}
		if len(pe.SafeMessage) > 260 {
			t.Fatalf("unsafe/unbounded message len=%d", len(pe.SafeMessage))
		}
	}
}
func TestProviderErrorRetryAfterHTTPDateAndTimeout(t *testing.T) {
	now := time.Now().UTC()
	h := http.Header{"Retry-After": []string{now.Add(4 * time.Second).Format(http.TimeFormat)}}
	pe := ProviderHTTPErrorAt("p", "embed", 429, h, nil, now).(*ProviderError)
	if pe.RetryAfter < 3*time.Second || pe.RetryAfter > 4*time.Second {
		t.Fatalf("retry=%v", pe.RetryAfter)
	}
	err := ProviderTransportError("p", "asr", timeoutErr{})
	var got *ProviderError
	if !errors.As(err, &got) || got.Class != ErrorTimeout || !got.Retryable {
		t.Fatalf("got=%+v", got)
	}
	if strings.Contains(got.Error(), "secret") {
		t.Fatal("cause leaked")
	}
	if !errors.Is(got, got.Cause) {
		t.Fatal("cause not unwrap-able")
	}
	_ = context.Background()
}
