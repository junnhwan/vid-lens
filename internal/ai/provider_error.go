package ai

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type ErrorClass string

const (
	ErrorRateLimited    ErrorClass = "rate_limited"
	ErrorProvider5xx    ErrorClass = "provider_5xx"
	ErrorTimeout        ErrorClass = "timeout"
	ErrorNetwork        ErrorClass = "network"
	ErrorAuth           ErrorClass = "auth"
	ErrorInvalidRequest ErrorClass = "invalid_request"
	ErrorNonRetryable   ErrorClass = "non_retryable"
)

type ProviderError struct {
	Provider, Operation    string
	Class                  ErrorClass
	StatusCode             int
	Retryable              bool
	RetryAfter             time.Duration
	RequestID, SafeMessage string
	Cause                  error
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("provider %s %s failed: %s (status=%d request_id=%s)", e.Provider, e.Operation, e.Class, e.StatusCode, e.RequestID)
}
func (e *ProviderError) Unwrap() error { return e.Cause }
func ProviderHTTPError(provider, operation string, status int, h http.Header, body []byte) error {
	return ProviderHTTPErrorAt(provider, operation, status, h, body, time.Now())
}
func ProviderHTTPErrorAt(provider, operation string, status int, h http.Header, body []byte, now time.Time) error {
	class, retry := ErrorNonRetryable, false
	switch {
	case status == 429:
		class, retry = ErrorRateLimited, true
	case status >= 500:
		class, retry = ErrorProvider5xx, true
	case status == 401 || status == 403:
		class = ErrorAuth
	case status >= 400 && status < 500:
		class = ErrorInvalidRequest
	}
	rid := firstHeader(h, "X-Request-ID", "Request-ID", "Trace-ID", "X-Amzn-RequestId")
	return &ProviderError{Provider: provider, Operation: operation, Class: class, StatusCode: status, Retryable: retry, RetryAfter: parseRetryAfter(h.Get("Retry-After"), now), RequestID: safeText(rid, 128), SafeMessage: safeText(string(body), 256)}
}
func ProviderTransportError(provider, operation string, cause error) error {
	class := ErrorNetwork
	var ne net.Error
	if errors.As(cause, &ne) && ne.Timeout() {
		class = ErrorTimeout
	}
	return &ProviderError{Provider: provider, Operation: operation, Class: class, Retryable: true, Cause: cause, SafeMessage: classString(class)}
}
func classString(c ErrorClass) string { return string(c) }
func firstHeader(h http.Header, names ...string) string {
	for _, n := range names {
		if v := strings.TrimSpace(h.Get(n)); v != "" {
			return v
		}
	}
	return ""
}
func parseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if n, e := strconv.Atoi(v); e == nil && n >= 0 {
		return time.Duration(n) * time.Second
	}
	if at, e := http.ParseTime(v); e == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}
func safeText(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if r < ' ' && r != '\t' {
			return -1
		}
		return r
	}, s)
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "…"
}
