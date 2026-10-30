package store

import (
	"context"
	"encoding/json"
	"testing"

	"vidlens/internal/domain"
)

func TestArticleLoopAdmissionDefaultsToV2AndKeepsExplicitHistoricalContracts(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	_, p, err := s.CurrentConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p.ArticleHarness = ""
	if _, err = s.SaveConfig(ctx, p, "", ""); err != nil {
		t.Fatal(err)
	}
	req, claim := prepared(t, s, owner)
	var original struct {
		Contract        domain.ExecutionContract `json:"execution_contract"`
		DeliveryPolicy  string                   `json:"article_delivery_policy"`
		QuestionsPolicy string                   `json:"article_questions_policy"`
	}
	_ = json.Unmarshal(claim.Run.Checkpoint, &original)
	if original.Contract.Version != domain.ArticleLoopV2 {
		t.Fatal("new article did not select the current independent loop")
	}
	if original.DeliveryPolicy != domain.CurrentArticleDelivery {
		t.Fatal("new article did not freeze delivery before advice")
	}
	if original.QuestionsPolicy != domain.ArticleQuestionsPolicy {
		t.Fatal("new article did not freeze optional questions preparation")
	}
	if _, err := s.Fail(ctx, claim, domain.Failure{Code: "MODEL_RESULT_INVALID", CanRetry: true}, false, 0); err != nil {
		t.Fatal(err)
	}
	_, p, err = s.CurrentConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p.ArticleHarness = domain.ArticleLoopV1
	if _, err = s.SaveConfig(ctx, p, "", ""); err != nil {
		t.Fatal(err)
	}
	next, nextClaim := prepared(t, s, owner, "https://youtube.com/watch?v=zyxwvutsrqp")
	var admitted struct {
		Contract domain.ExecutionContract `json:"execution_contract"`
	}
	_ = json.Unmarshal(nextClaim.Run.Checkpoint, &admitted)
	if admitted.Contract.Version != domain.ArticleLoopV1 || admitted.Contract.RunID != nextClaim.Run.ID {
		t.Fatal("explicit article adapter not frozen", string(nextClaim.Run.Checkpoint))
	}
	if err = s.Publish(ctx, nextClaim, domain.Article{SnapshotID: *next.SnapshotID}); err == nil {
		t.Fatal("new adapter bypassed approval using direct Store.Publish")
	}
	_ = req
}
func TestArticleLoopPhysicalBudgetIsTransactional(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	_, p, _ := s.CurrentConfig(ctx)
	p.ArticleHarness = domain.ArticleLoopV1
	p.MaxIterations = 8
	if _, err := s.SaveConfig(ctx, p, "", ""); err != nil {
		t.Fatal(err)
	}
	_, claim := prepared(t, s, owner)
	for i := 0; i < 8; i++ {
		if _, err := s.BeginModelSubmission(ctx, claim, "unknown:"+string(rune('a'+i)), map[string]int{"n": i}, claim.Run.ConfigID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.BeginModelSubmission(ctx, claim, "one-too-many", nil, claim.Run.ConfigID); err == nil {
		t.Fatal("physical total ceiling bypassed via another logical key")
	}
	stats, err := s.ModelExecution(ctx, claim.Run.ID)
	if err != nil || stats.Submissions != 8 {
		t.Fatalf("unknown sends lost from total: %+v %v", stats, err)
	}
}
