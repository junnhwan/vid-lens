package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"vidlens/internal/domain"
)

func TestIdenticalDraftContentKeepsVersionAndReplaysItsOwnResult(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	request, claim := prepared(t, s, owner)
	first := map[string]any{"title": "来源标题", "sections": map[string]any{"one": "来源事实"}, "batch_receipt": map[string]any{"accepted_sections": []string{"one"}}}
	if saved, err := s.SaveArticleDraft(ctx, claim, "initial", "initial", 0, first); err != nil || saved.Version != 1 {
		t.Fatal(saved, err)
	}
	same := map[string]any{"title": "来源标题", "sections": map[string]any{"one": "来源事实"}, "batch_receipt": map[string]any{"accepted_sections": []string{"one"}, "ignored_unplanned_sections": []string{"extra"}}}
	noop, err := s.SaveArticleDraft(ctx, claim, "repeat", "repeat", 1, same)
	if err != nil || noop.Version != 1 {
		t.Fatalf("a changed acknowledgement spent a content version: %+v %v", noop, err)
	}
	var result map[string]any
	_ = json.Unmarshal(domain.JSON(noop), &result)
	if result["noop"] != true {
		t.Fatal("unchanged content was not reported as a noop", result)
	}
	latest, err := s.ArticleDraft(ctx, claim.Run.ID)
	if err != nil || latest.Version != 1 || !draftJSONEqual(latest.Data, domain.JSON(first)) {
		t.Fatal("a noop replaced the authoritative revision", latest, err)
	}
	changed := map[string]any{"title": "来源标题", "sections": map[string]any{"one": "实际修正后的条件"}}
	if saved, err := s.SaveArticleDraft(ctx, claim, "changed", "changed", 1, changed); err != nil || saved.Version != 2 {
		t.Fatal(saved, err)
	}
	replayed, err := s.SaveArticleDraft(ctx, claim, "repeat", "repeat", 1, changed)
	if err != nil || replayed.Version != 1 || !draftJSONEqual(replayed.Data, noop.Data) {
		t.Fatal("noop replay returned a later revision or re-executed the mutation", replayed, err)
	}
	if _, err = s.SaveArticleDraft(ctx, claim, "stale-repeat", nil, 1, changed); err == nil {
		t.Fatal("a stale write bypassed CAS because its content matched")
	}
	if _, err = s.SaveArticleDraft(ctx, claim, "repeat", "different-arguments", 1, same); err == nil {
		t.Fatal("noop replay accepted different arguments")
	}
	if err = s.CancelRequest(ctx, owner, request.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveArticleDraft(ctx, claim, "cancelled-noop", nil, 2, changed); !errors.Is(err, domain.ErrFenced) {
		t.Fatal("unchanged content bypassed cancellation fencing", err)
	}
}
