package jobs

import (
	"testing"
	"vidlens/internal/domain"
)

func TestFrozenBudgetFailureRequiresNewRequest(t *testing.T) {
	problem := &domain.Problem{Status: 422, Code: "RESOURCE_LIMIT_EXCEEDED", Detail: "当前冻结模型预算不足，请新建解读。", Meta: map[string]any{"can_retry": false}}
	failure, temporary, _ := classify(problem)
	if temporary || failure.CanRetry || failure.Message != problem.Detail {
		t.Fatal("impossible frozen-budget retry advertised", failure, temporary)
	}
	failure, _, _ = classify(domain.Err(422, "RESOURCE_LIMIT_EXCEEDED", "temporary admission limit"))
	if !failure.CanRetry {
		t.Fatal("unrelated resource limits lost retry semantics")
	}
}
