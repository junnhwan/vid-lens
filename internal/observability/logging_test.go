package observability

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestStructuredLogAddsCorrelationAndRedactsSensitiveFields(t *testing.T) {
	var out bytes.Buffer
	logger := NewJSONLogger(&out, slog.LevelDebug)
	ctx := WithCorrelation(context.Background(), Correlation{TraceID: "trace-1", TaskID: 42, JobID: 9, UserID: 7, JobType: "analyze", Stage: "summarizing", Attempt: 2})
	Log(ctx, logger, slog.LevelError, "provider failed",
		slog.String("api_key", "sk-secret-value"),
		slog.String("authorization", "Bearer bearer-secret"),
		slog.String("prompt", "完整 prompt 不得输出"),
		slog.String("transcript", "完整转写不得输出"),
		slog.String("error", SafeError(errors.New("request failed Authorization: Bearer abc123?api_key=sk-query-secret"))),
	)
	text := out.String()
	for _, secret := range []string{"sk-secret-value", "bearer-secret", "完整 prompt 不得输出", "完整转写不得输出", "sk-query-secret", "abc123"} {
		if strings.Contains(text, secret) {
			t.Fatalf("log leaked %q: %s", secret, text)
		}
	}
	for _, expected := range []string{`"trace_id":"trace-1"`, `"task_id":42`, `"job_id":9`, `"stage":"summarizing"`, `"attempt":2`, `[REDACTED]`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("log missing %q: %s", expected, text)
		}
	}
}

func TestSafeErrorTruncatesLongProviderError(t *testing.T) {
	got := SafeError(errors.New(strings.Repeat("x", 700)))
	if len(got) > 500 {
		t.Fatalf("SafeError length=%d", len(got))
	}
}

func TestSafeErrorRemovesURLQueryAndFragment(t *testing.T) {
	got := SafeError(errors.New("yt-dlp failed for https://video.example/watch?v=secret-token&cookie=session-secret#frag: stderr body"))
	for _, secret := range []string{"secret-token", "session-secret", "frag"} {
		if strings.Contains(got, secret) {
			t.Fatalf("SafeError leaked %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "https://video.example/watch?[REDACTED]") {
		t.Fatalf("SafeError did not preserve a diagnosable URL origin/path: %s", got)
	}
}

func TestStructuredLogRedactsGeneratedTitleAndLegacyMessageSecrets(t *testing.T) {
	var out bytes.Buffer
	logger := NewJSONLogger(&out, slog.LevelDebug)
	Log(context.Background(), logger, slog.LevelInfo, "video title generated",
		slog.String("title", "private generated title"),
		slog.String("error", "request https://example.test/path?token=query-secret failed"),
	)
	text := out.String()
	for _, secret := range []string{"private generated title", "query-secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("structured log leaked %q: %s", secret, text)
		}
	}
}
