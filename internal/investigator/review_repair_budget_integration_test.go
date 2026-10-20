package investigator

import (
	"context"
	"fmt"
	"testing"

	"vidlens/internal/domain"
)

func TestReviewCorrectionAndItsPatchesShareTheAuthorRepairBudget(t *testing.T) {
	s := deterministicDraftSession(t)
	ctx := context.Background()
	for round := 0; round < s.repairLimit(); round++ {
		_, draft, err := s.loadDraft(ctx)
		if err != nil {
			t.Fatal(err)
		}
		article, err := s.assembleArticle(draft)
		if err != nil {
			t.Fatal(err)
		}
		issue := evidenceReviewCorrection(map[string]any{"sections": article.Sections}, fmt.Sprintf("review-%d", round), []map[string]any{{"path": "/sections/1/blocks/0/content/0/text", "reason": "修正来源中的条件。", "citation_ids": []string{"t25"}}})
		s.cp.EvidenceReviewIssue = issue
		if err = s.repair(ctx, &domain.Problem{Status: 422, Code: "MODEL_RESULT_INVALID", Detail: "独立复核要求修订。", Meta: issue}); err != nil {
			t.Fatal("logical author correction was charged twice", round, err)
		}
		input := draftPatchInput{Version: 2 + round, Patches: []fieldPatch{{Op: "replace", Path: "/sections/finish/blocks/0/content/0/text", Value: domain.JSON(fmt.Sprintf("来源中第%d个有效条件。", round+1))}}}
		result, err := planCall(s, fmt.Sprintf("review-patch-%d", round), "patch_article_draft", input)
		if err != nil || result["error"] != nil || result["version"] != float64(3+round) {
			t.Fatal("saving a charged author correction spent the quota a second time", round, result, err)
		}
		if _, err = planCall(s, fmt.Sprintf("review-patch-%d", round), "patch_article_draft", input); err != nil {
			t.Fatal("replaying a completed patch charged another repair", err)
		}
	}
	if used, err := s.repairUsage(ctx); err != nil || used != s.repairLimit() {
		t.Fatal("logical repairs did not retain the original global limit", used, err)
	}
	if err := s.repair(ctx, invalid("仍然需要第四轮修订")); err == nil {
		t.Fatal("the fix granted an extra author correction")
	}
}

func TestUnchargedReviewFindingDoesNotGrantFreeFieldRepairs(t *testing.T) {
	s := deterministicDraftSession(t)
	s.cp.Repairs = s.repairLimit()
	s.cp.EvidenceReviewIssue = map[string]any{"candidate_hash": "new-unpaid-review", "code": "semantic_evidence"}
	s.cp.FinalizationFailure = &domain.Problem{Code: "MODEL_RESULT_INVALID", Meta: map[string]any{"candidate_hash": "different-paid-review"}}
	input := draftPatchInput{Version: 2, Patches: []fieldPatch{{Op: "replace", Path: "/sections/finish/blocks/0/content/0/text", Value: domain.JSON("不能绕过原有修订上限。")}}}
	if _, err := planCall(s, "unpaid-patch", "patch_article_draft", input); err == nil {
		t.Fatal("an uncharged review identity granted a free patch")
	}
	if version, _, err := s.loadDraft(context.Background()); err != nil || version != 2 {
		t.Fatal("a rejected patch changed the saved draft", version, err)
	}
}

func TestChargedReviewStillBoundsNoopPatchAttemptsAfterReplay(t *testing.T) {
	s := deterministicDraftSession(t)
	ctx := context.Background()
	issue := map[string]any{"candidate_hash": "one-paid-review", "code": "semantic_evidence"}
	s.cp.EvidenceReviewIssue = issue
	if err := s.repair(ctx, &domain.Problem{Status: 422, Code: "MODEL_RESULT_INVALID", Detail: "需要修订。", Meta: issue}); err != nil {
		t.Fatal(err)
	}
	input := draftPatchInput{Version: 2, Patches: []fieldPatch{{Op: "replace", Path: "/sections/finish/blocks/0/content/0/text", Value: domain.JSON("结束事实。")}}}
	for attempt := 0; attempt < s.repairLimit(); attempt++ {
		id := fmt.Sprintf("noop-patch-%d", attempt)
		if result, err := planCall(s, id, "patch_article_draft", input); err != nil || result["outcome"] != "noop" {
			t.Fatal(result, err)
		}
		if _, err := planCall(s, id, "patch_article_draft", input); err != nil {
			t.Fatal("replay used another patch attempt", err)
		}
	}
	if _, err := planCall(s, "extra-noop-patch", "patch_article_draft", input); err == nil {
		t.Fatal("a charged correction granted unlimited patch attempts")
	}
	if used, err := s.repairUsage(ctx); err != nil || used != 1 {
		t.Fatal("patch attempts spent new logical author corrections", used, err)
	}
}
