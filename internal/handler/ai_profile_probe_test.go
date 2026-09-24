package handler

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"vid-lens/internal/ai"
)

func TestProbeErrorsNeverEchoProviderBodyOrRequestURL(t *testing.T) {
	secret := "sk-super-secret"
	provider := ai.ProviderHTTPError("openai", "chat", http.StatusBadRequest, http.Header{}, []byte("invalid "+secret))
	if message := safeProbeError(provider); strings.Contains(message, secret) || !strings.Contains(message, "模型") {
		t.Fatalf("unsafe provider error: %q", message)
	}
	transport := ai.ProviderTransportError("openai", "chat", errors.New("https://api.example/v1?key="+secret))
	if message := safeProbeError(transport); strings.Contains(message, secret) {
		t.Fatalf("unsafe transport error: %q", message)
	}
}
