package jobs

import (
	"testing"
	"vidlens/internal/domain"
)

func TestArticleLoopFailureOffersNewRequestWithoutAutomaticBudgetReset(t *testing.T) {
	for _, code := range []string{"ARTICLE_BUDGET_EXHAUSTED", "ARTICLE_NO_PROGRESS", "ARTICLE_RESOURCE_EXHAUSTED", "ARTICLE_CAPACITY_EXCEEDED", "ARTICLE_CANDIDATE_CHANGED", "ARTICLE_EVIDENCE_CHANGED", "ARTICLE_REPAIR_LIMIT", "ARTICLE_REVIEW_PROTOCOL_FAILED"} {
		failure, automatic, _ := classify(domain.Err(422, code, "retained workspace"))
		if !domain.RetryableFailure(&failure) || automatic {
			t.Fatalf("%s must permit explicit recovery only: %+v automatic=%v", code, failure, automatic)
		}
	}
	for _, code := range []string{"ARTICLE_CANNOT_COMPLETE", "ARTICLE_APPROVAL_REQUIRED"} {
		failure, automatic, _ := classify(domain.Err(422, code, "requires intervention"))
		if failure.CanRetry || automatic {
			t.Fatalf("%s must not imply a recoverable budget stop", code)
		}
	}
}
