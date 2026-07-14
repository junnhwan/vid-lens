package quota

import "testing"

func TestOperationFailurePolicyValidationAndResolution(t *testing.T) {
	p := OperationPolicies{Default: FailOpen, Operations: map[string]FailurePolicy{"chat": FailClosed, "asr": FailConservativeLocal}}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if p.Policy("chat") != FailClosed || p.Policy("health") != FailOpen || p.Policy("asr") != FailConservativeLocal {
		t.Fatalf("policy mismatch")
	}
	bad := OperationPolicies{Default: FailurePolicy(99)}
	if bad.Validate() == nil {
		t.Fatal("expected invalid policy")
	}
}
