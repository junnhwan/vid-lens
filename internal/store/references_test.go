package store

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"vidlens/internal/domain"
)

func referenceTurn(t *testing.T, s *Store) (string, domain.Turn, domain.Claim) {
	t.Helper()
	ctx := context.Background()
	owner := session(t, s)
	r, c := prepared(t, s, owner)
	if e := s.Publish(ctx, c, domain.Article{SnapshotID: *r.SnapshotID, Title: "fixture", Sections: []domain.Section{}, Citations: map[string]domain.Citation{}}); e != nil {
		t.Fatal(e)
	}
	con, _, e := s.EnsureConversation(ctx, owner, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	a, e := s.CreateTurn(ctx, owner, con.ID, domain.ID(), domain.QuestionInput{Question: "check a reference", AllowExternalSources: true}, "")
	if e != nil {
		t.Fatal(e)
	}
	turn, e := s.Turn(ctx, owner, con.ID, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	claim, e := s.Claim(ctx, turn.RunID, 1, "references")
	if e != nil || claim == nil {
		t.Fatal(e)
	}
	return owner, turn, *claim
}
func TestReferenceReadBudgetIsAtomicAndRedirectSnapshotIsStable(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner, turn, c := referenceTurn(t, s)
	refs := make([]Reference, 8)
	for i := range refs {
		r, e := s.RegisterReference(ctx, c, turn, fmt.Sprintf("https://example.com/%d", i), "")
		if e != nil {
			t.Fatal(e)
		}
		refs[i] = r
	}
	var wg sync.WaitGroup
	var sent atomic.Int32
	for i, r := range refs {
		wg.Add(1)
		go func(i int, r Reference) {
			defer wg.Done()
			if _, _, e := s.StartReferenceRead(ctx, c, r.ID, fmt.Sprint("read", i)); e == nil {
				sent.Add(1)
			}
		}(i, r)
	}
	wg.Wait()
	if sent.Load() != 3 {
		t.Fatalf("read budget permitted %d submissions", sent.Load())
	}
	if e := s.SaveReference(ctx, c, refs[0].ID, "read0", "Final title", refs[1].URL, "actual source text", "hash", false); e != nil {
		t.Fatal(e)
	}
	r, e := s.ReferenceForRun(ctx, c.Run.ID, refs[0].ID)
	if e != nil || r.URL != refs[0].URL || r.EvidenceURL() != refs[1].URL {
		t.Fatalf("redirect lost registration or collided with another URL: %+v %v", r, e)
	}
	if _, e = s.PublicReference(ctx, session(t, s), turn.RequestID, r.ID); e == nil {
		t.Fatal("cross-session snapshot exposed")
	}
	if _, e = s.PublicReference(ctx, owner, turn.RequestID, r.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, "UPDATE external_references SET expires_at=now()-interval '1 second' WHERE id=$1", r.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.CleanupMetadata(ctx); e != nil {
		t.Fatal(e)
	}
	var body *string
	if e = s.Pool.QueryRow(ctx, "SELECT body FROM external_references WHERE id=$1", r.ID).Scan(&body); e != nil || body != nil {
		t.Fatal("expired private snapshot body retained")
	}
}
func TestSearchCreditReservationDoesNotReplayUnknownOrExceedMonth(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	_, _, c := referenceTurn(t, s)
	for i := 0; i < 8; i++ {
		if _, e := s.BeginOperation(ctx, c, fmt.Sprint("search", i), "web.search", map[string]string{"q": "fixture"}, false); e != nil {
			t.Fatal(e)
		}
	}
	var wg sync.WaitGroup
	var reserved atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if e := s.ReserveSearch(ctx, c, fmt.Sprint("search", i), 1); e == nil {
				reserved.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if reserved.Load() != 1 {
		t.Fatalf("monthly free quota reserved %d", reserved.Load())
	}
	var key string
	if e := s.Pool.QueryRow(ctx, "SELECT operation_key FROM external_search_credits").Scan(&key); e != nil {
		t.Fatal(e)
	}
	if e := s.ReserveSearch(ctx, c, key, 1000); e == nil {
		t.Fatal("unknown physical search was resent")
	}
	if e := s.ExhaustSearchMonth(ctx); e != nil {
		t.Fatal(e)
	}
	if e := s.ReserveSearch(ctx, c, "search7", 1000); e == nil {
		t.Fatal("exhausted month accepted search")
	}
	var n int
	_ = s.Pool.QueryRow(ctx, "SELECT count(*) FROM external_search_credits").Scan(&n)
	if n != 1 {
		t.Fatal("unknown result was refunded")
	}
}
