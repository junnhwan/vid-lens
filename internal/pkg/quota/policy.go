package quota

import "fmt"

// OperationPolicies makes the Redis outage behavior explicit per AI operation.
// Ordinary endpoints may fail open; expensive operations should fail closed or
// use a small process-local conservative allowance.
type OperationPolicies struct {
	Default    FailurePolicy
	Operations map[string]FailurePolicy
}

func (p OperationPolicies) Policy(operation string) FailurePolicy {
	if x, ok := p.Operations[operation]; ok {
		return x
	}
	return p.Default
}
func (p OperationPolicies) Validate() error {
	if !validFailurePolicy(p.Default) {
		return fmt.Errorf("invalid default quota failure policy: %d", p.Default)
	}
	for op, policy := range p.Operations {
		if op == "" || !validFailurePolicy(policy) {
			return fmt.Errorf("invalid quota failure policy for %q: %d", op, policy)
		}
	}
	return nil
}
func validFailurePolicy(p FailurePolicy) bool {
	return p == FailOpen || p == FailClosed || p == FailConservativeLocal
}
