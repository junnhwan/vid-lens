package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"vidlens/internal/domain"
)

func TestShareCapabilityRejectsTamperingWithoutDatabaseAccess(t *testing.T) {
	s := &Store{signing: []byte("test-share-signing-secret")}
	id := domain.ID()
	token := s.shareToken(id)
	if got, err := s.shareID(token); err != nil || got != id {
		t.Fatalf("capability round trip: %s %v", got, err)
	}
	other := &Store{signing: []byte("different-secret")}
	for _, invalid := range []string{"", id, id + ".", token + "x", other.shareToken(id), "../" + token, strings.Repeat("x", 1000)} {
		if _, err := s.SharedRecord(context.Background(), invalid); !notFound(err) {
			t.Fatalf("invalid token accepted: %v", err)
		}
		if _, err := s.SharedFrame(context.Background(), invalid, domain.ID()); !notFound(err) {
			t.Fatalf("invalid frame token accepted: %v", err)
		}
	}
}

func TestSharedRecordIsReadOnlyRevocableAndExcludesPrivateQuestions(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner, other := session(t, s), session(t, s)
	r, claim := prepared(t, s, owner)
	frameID, privateFrameID := domain.ID(), domain.ID()
	for _, id := range []string{frameID, privateFrameID} {
		if err := s.AddAsset(ctx, claim, domain.Asset{ID: id, SnapshotID: r.SnapshotID, SourceID: r.SourceID, RunID: claim.Run.ID, Kind: "frame", StorageKey: "private-storage/" + id + ".jpg", SHA256: "hash", MIME: "image/jpeg", Width: 32, Height: 18, Bytes: 10}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 12; i++ {
		_, err := s.Step(ctx, claim, domain.Step{ID: domain.ID(), Phase: "compose", Title: "写入公开步骤", Kind: "tool_execution", Status: "completed", FrameIDs: []string{frameID}})
		if err != nil {
			t.Fatal(err)
		}
	}
	before, err := s.RequestShare(ctx, owner, r.ID)
	if err != nil || before.Enabled || before.URL != nil {
		t.Fatalf("unexpected default: %+v %v", before, err)
	}
	for _, enabled := range []bool{true, false} {
		if _, err := s.SetRequestShare(ctx, other, r.ID, enabled); !notFound(err) {
			t.Fatalf("other visitor changed share: %v", err)
		}
	}
	if _, err := s.RequestShare(ctx, other, r.ID); !notFound(err) {
		t.Fatalf("other visitor read capability: %v", err)
	}
	share, err := s.SetRequestShare(ctx, owner, r.ID, true)
	if err != nil || !share.Enabled || share.URL == nil {
		t.Fatalf("create share: %+v %v", share, err)
	}
	token := strings.TrimPrefix(*share.URL, "/s/")
	if same, err := s.SetRequestShare(ctx, owner, r.ID, true); err != nil || *same.URL != *share.URL {
		t.Fatal("enable replay changed the share capability")
	}
	active, err := s.SharedRecord(ctx, token)
	if err != nil || active.Article != nil || len(active.Steps) != 12 {
		t.Fatalf("running share or full history: %+v %v", active, err)
	}
	a := domain.Article{SchemaVersion: 1, SnapshotID: *r.SnapshotID, Title: "共享图文", Source: domain.Source{Platform: "youtube", CanonicalURL: "https://youtube.com/watch?v=abcdefghijk"}, Sections: []domain.Section{{ID: "section", Title: "公开章节", Blocks: []domain.Block{{ID: "figure", Type: "figure", FrameIDs: []string{frameID}}}}}, Citations: map[string]domain.Citation{}, Limitations: []domain.Limitation{}}
	if err := s.Publish(ctx, claim, a); err != nil {
		t.Fatal(err)
	}
	conversation, _, err := s.EnsureConversation(ctx, owner, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTurn(ctx, owner, conversation.ID, domain.ID(), domain.QuestionInput{Question: "NEVER_SHARE_THIS_PRIVATE_QUESTION"}, ""); err != nil {
		t.Fatal(err)
	}
	shared, err := s.SharedRecord(ctx, token)
	if err != nil || shared.Article == nil || len(shared.Steps) != 12 {
		t.Fatalf("published share: %+v %v", shared, err)
	}
	if shared.View["actions"].(map[string]bool)["can_cancel"] || shared.View["actions"].(map[string]bool)["can_retry"] {
		t.Fatal("public share offers mutations")
	}
	raw, err := json.Marshal(shared)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{owner, other, conversation.ID, r.ConfigID, "NEVER_SHARE_THIS_PRIVATE_QUESTION", privateFrameID, "private-storage/", "session_id", "csrf_token", "stream", "/api/v1/requests/"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("public share leaked %s", forbidden)
		}
	}
	if asset, err := s.SharedFrame(ctx, token, frameID); err != nil || asset.ID != frameID {
		t.Fatalf("shared figure: %+v %v", asset, err)
	}
	if _, err := s.SharedFrame(ctx, token, privateFrameID); !notFound(err) {
		t.Fatalf("unreferenced frame exposed: %v", err)
	}
	if _, err := s.Request(ctx, other, r.ID); !notFound(err) {
		t.Fatalf("sharing grants private ownership: %v", err)
	}
	if canceled, err := s.SetRequestShare(ctx, owner, r.ID, false); err != nil || canceled.Enabled || canceled.URL != nil {
		t.Fatalf("revoke: %+v %v", canceled, err)
	}
	if _, err := s.SharedRecord(ctx, token); !notFound(err) {
		t.Fatalf("revoked record readable: %v", err)
	}
	if _, err := s.SharedFrame(ctx, token, frameID); !notFound(err) {
		t.Fatalf("revoked picture readable: %v", err)
	}
	reopened, err := s.SetRequestShare(ctx, owner, r.ID, true)
	if err != nil || *reopened.URL == *share.URL {
		t.Fatalf("re-sharing resurrected the old link: %+v %v", reopened, err)
	}
	if _, err := s.SharedRecord(ctx, token); !notFound(err) {
		t.Fatalf("old link revived: %v", err)
	}
}

func TestShareRetainsFrozenCancellationAndDeduplicatesOwnedRetryHistory(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner, other := session(t, s), session(t, s)
	first, err := s.CreateRequest(ctx, owner, "first", "https://youtube.com/watch?v=abcdefghijk", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRequest(ctx, other, "other", "https://youtube.com/watch?v=abcdefghijk", "", false); err != nil {
		t.Fatal(err)
	}
	r, err := s.Request(ctx, owner, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := s.Claim(ctx, *r.ActiveRunID, 1, "shared-source")
	if err != nil || claim == nil {
		t.Fatal(err)
	}
	earlier, err := s.Step(ctx, *claim, domain.Step{ID: domain.ID(), Phase: "prepare_source", Title: "原记录可见步骤", Status: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CancelRequest(ctx, owner, first.ID); err != nil {
		t.Fatal(err)
	}
	later, err := s.Step(ctx, *claim, domain.Step{ID: domain.ID(), Phase: "prepare_source", Title: "取消后的新步骤", Status: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	share, err := s.SetRequestShare(ctx, owner, first.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := s.SharedRecord(ctx, strings.TrimPrefix(*share.URL, "/s/"))
	if err != nil || len(frozen.Steps) != 1 || frozen.Steps[0].ID != earlier.ID {
		t.Fatalf("canceled history changed: %+v %v", frozen.Steps, err)
	}
	retry, err := s.CreateRequest(ctx, owner, "retry", "", first.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	share, err = s.SetRequestShare(ctx, owner, retry.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := s.SharedRecord(ctx, strings.TrimPrefix(*share.URL, "/s/"))
	if err != nil || len(resumed.Steps) != 2 || resumed.Steps[1].ID != later.ID {
		t.Fatalf("retry history lost or duplicated: %+v %v", resumed.Steps, err)
	}
}
