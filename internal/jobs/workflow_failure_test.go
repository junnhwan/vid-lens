package jobs

import (
	"testing"

	"vidlens/internal/domain"
)

func TestWorkflowProviderFailureKeepsExplicitRetryWithoutAutomaticResend(t *testing.T) {
	failure, automatic, _ := classify(&domain.Problem{Status: 502, Code: "PROVIDER_UNAVAILABLE", Detail: "单次响应超时，结果未知。", Meta: map[string]any{"can_retry": true, "retry_action": "resume_checkpoint"}})
	if failure.Code != "PROVIDER_UNAVAILABLE" || !failure.CanRetry || automatic {
		t.Fatal("provider interruption lost explicit recovery or scheduled an identical resend", failure, automatic)
	}
}
