package investigator

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vidlens/internal/domain"
)

func TestProviderContextFailureIsTerminalAndRedactedForBusinessCalls(t *testing.T) {
	const secret = "gateway-private-token-that-must-not-be-stored"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		fmt.Fprintf(w, `{"error":{"code":"context_length_exceeded","message":"This request is longer than the model's context window. token=%s"}}`, secret)
	}))
	defer server.Close()
	var report usageReport
	o := newUsageObserver("test", "", func(r usageReport) { report = r })
	ctx := context.WithValue(context.Background(), usageKey{}, o)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	_, err := (safeTransport{http.DefaultTransport}).RoundTrip(req)
	var work *domain.WorkError
	if !errors.As(err, &work) || work.Code != "MODEL_CONTEXT_EXCEEDED" || work.Transient {
		t.Fatalf("context rejection was treated as a network retry: %v", err)
	}
	if report.HTTPStatus == nil || *report.HTTPStatus != 400 || report.ProviderErrorCode != "context_length_exceeded" || strings.Contains(report.ProviderErrorMessage, secret) || !strings.Contains(report.ProviderErrorMessage, "[redacted]") {
		t.Fatalf("business diagnostic missing or credential leaked: %+v", report)
	}
	if report.Input.Status != "unknown" || report.Total.Status != "unknown" {
		t.Fatal("a rejected request invented billing usage")
	}
}

func TestContextClassificationDoesNotGuessFromOutputOrOtherFailures(t *testing.T) {
	for _, body := range []string{
		`{"error":{"message":"max_tokens is above the output limit"}}`,
		`{"error":{"message":"rate limit exceeded"}}`,
		`{"error":{"message":"image format unsupported"}}`,
		`<html>context_length_exceeded</html>`,
	} {
		if providerContextExceeded([]byte(body)) {
			t.Fatal("unrelated provider rejection was classified as context", body)
		}
	}
}
