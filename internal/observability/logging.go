package observability

import (
	"context"
	"io"
	"log/slog"
	"regexp"
	"strings"
)

const maxSafeErrorLength = 500

var (
	bearerPattern = regexp.MustCompile(`(?i)(bearer\s+)[^\s"']+`)
	apiKeyPattern = regexp.MustCompile(`(?i)((?:api[_-]?key|token|access[_-]?token)\s*[=:]\s*)[^\s&"']+`)
	skPattern     = regexp.MustCompile(`\bsk-[A-Za-z0-9._-]+`)
	urlPattern    = regexp.MustCompile(`https?://[^\s"']+`)
)

var sensitiveLogKeys = map[string]struct{}{
	"api_key": {}, "apikey": {}, "authorization": {}, "cookie": {},
	"prompt": {}, "messages": {}, "transcript": {}, "transcription": {}, "content": {}, "title": {},
}

func NewJSONLogger(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}))
}

// Log emits a JSON record enriched with business correlation fields. Sensitive
// values are removed by key and provider errors should be passed through SafeError.
func Log(ctx context.Context, logger *slog.Logger, level slog.Level, msg string, attrs ...slog.Attr) {
	if logger == nil {
		return
	}
	correlation := CorrelationFromContext(ctx)
	safe := make([]slog.Attr, 0, len(attrs)+7)
	if correlation.TraceID != "" {
		safe = append(safe, slog.String("trace_id", correlation.TraceID))
	}
	if correlation.TaskID != 0 {
		safe = append(safe, slog.Int64("task_id", correlation.TaskID))
	}
	if correlation.JobID != 0 {
		safe = append(safe, slog.Int64("job_id", correlation.JobID))
	}
	if correlation.UserID != 0 {
		safe = append(safe, slog.Int64("user_id", correlation.UserID))
	}
	if correlation.JobType != "" {
		safe = append(safe, slog.String("job_type", correlation.JobType))
	}
	if correlation.Stage != "" {
		safe = append(safe, slog.String("stage", correlation.Stage))
	}
	if correlation.Attempt != 0 {
		safe = append(safe, slog.Int("attempt", correlation.Attempt))
	}
	for _, attr := range attrs {
		safe = append(safe, sanitizeAttr(attr))
	}
	logger.LogAttrs(ctx, level, msg, safe...)
}

func sanitizeAttr(attr slog.Attr) slog.Attr {
	key := strings.ToLower(strings.TrimSpace(attr.Key))
	if _, sensitive := sensitiveLogKeys[key]; sensitive {
		return slog.String(attr.Key, "[REDACTED]")
	}
	if attr.Value.Kind() == slog.KindGroup {
		group := attr.Value.Group()
		clean := make([]slog.Attr, 0, len(group))
		for _, child := range group {
			clean = append(clean, sanitizeAttr(child))
		}
		return slog.Group(attr.Key, attrsToAny(clean)...)
	}
	if attr.Value.Kind() == slog.KindString && (key == "error" || strings.HasSuffix(key, "_error") || strings.HasSuffix(key, "_msg")) {
		return slog.String(attr.Key, sanitizeErrorText(attr.Value.String()))
	}
	return attr
}

func attrsToAny(attrs []slog.Attr) []any {
	values := make([]any, len(attrs))
	for i := range attrs {
		values[i] = attrs[i]
	}
	return values
}

func SafeError(err error) string {
	if err == nil {
		return ""
	}
	return sanitizeErrorText(err.Error())
}

func sanitizeErrorText(text string) string {
	text = strings.TrimSpace(text)
	text = bearerPattern.ReplaceAllString(text, `${1}[REDACTED]`)
	text = apiKeyPattern.ReplaceAllString(text, `${1}[REDACTED]`)
	text = skPattern.ReplaceAllString(text, `[REDACTED]`)
	text = urlPattern.ReplaceAllStringFunc(text, sanitizeURLText)
	if len(text) > maxSafeErrorLength {
		text = text[:maxSafeErrorLength]
	}
	return text
}

func sanitizeURLText(raw string) string {
	query := strings.IndexByte(raw, '?')
	fragment := strings.IndexByte(raw, '#')
	cut := len(raw)
	if query >= 0 && query < cut {
		cut = query
	}
	if fragment >= 0 && fragment < cut {
		cut = fragment
	}
	if cut == len(raw) {
		return raw
	}
	return raw[:cut] + "?[REDACTED]"
}
