package investigator

import (
	"context"
	"errors"
	"testing"

	"vidlens/internal/domain"
)

func TestProbeFailuresKeepSupplierAndCapabilityEvidenceDistinct(t *testing.T) {
	for _, tc := range []struct {
		name            string
		err, contextErr error
		report          usageReport
		want            string
	}{
		{"cooldown", errors.New("provider failure"), nil, usageReport{HTTPStatus: domain.Ptr(429), ProviderErrorCode: "account_cooldown"}, "PROVIDER_ACCOUNT_COOLDOWN"},
		{"rate_limit", errors.New("provider failure"), nil, usageReport{HTTPStatus: domain.Ptr(429)}, "PROVIDER_RATE_LIMITED"},
		{"missing_channel", errors.New("provider failure"), nil, usageReport{HTTPStatus: domain.Ptr(503), ProviderErrorCode: "model_not_found"}, "PROVIDER_MODEL_UNAVAILABLE"},
		{"auth", errors.New("provider failure"), nil, usageReport{HTTPStatus: domain.Ptr(401)}, "PROVIDER_AUTH_FAILED"},
		{"unsupported_request", errors.New("provider failure"), nil, usageReport{HTTPStatus: domain.Ptr(400)}, "PROVIDER_REQUEST_REJECTED"},
		{"timeout", capabilityFailure("CAPABILITY_MODEL_CALL_FAILED", context.DeadlineExceeded), context.DeadlineExceeded, usageReport{}, "PROBE_EXECUTION_TIMEOUT"},
		{"connection", errors.New("connection failed"), nil, usageReport{}, "PROVIDER_CONNECTION_FAILED"},
		{"wrong_pixels", capabilityFailure("CAPABILITY_OBSERVATION_MISMATCH", errors.New("wrong fact")), nil, usageReport{HTTPStatus: domain.Ptr(200), StreamStatus: "complete"}, "CAPABILITY_OBSERVATION_MISMATCH"},
		{"definite_rejection_before_deadline", errors.New("provider failure"), context.DeadlineExceeded, usageReport{HTTPStatus: domain.Ptr(429)}, "PROVIDER_RATE_LIMITED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := probeFailureCode("capabilities", tc.err, tc.contextErr, []usageReport{tc.report}); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}
