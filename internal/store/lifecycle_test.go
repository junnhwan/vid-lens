package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"vidlens/internal/domain"
)

func prepared(t *testing.T, s *Store, owner string) (domain.Request, domain.Claim) {
	t.Helper()
	ctx := context.Background()
	a, e := s.CreateRequest(ctx, owner, domain.ID(), "https://youtube.com/watch?v=abcdefghijk", "", false)
	if e != nil {
		t.Fatal(e)
	}
	r, _ := s.Request(ctx, owner, a.ID)
	c, e := s.Claim(ctx, *r.ActiveRunID, 1, "prepare")
	if e != nil || c == nil {
		t.Fatal(e)
	}
	video := domain.Asset{ID: domain.ID(), SourceID: c.Run.SourceID, RunID: c.Run.ID, Kind: "video", StorageKey: domain.ID() + ".mp4", SHA256: "video-hash", MIME: "video/mp4", Bytes: 42, ExpiresAt: domain.Ptr(time.Now().Add(time.Hour))}
	if e = s.AddAsset(ctx, *c, video); e != nil {
		t.Fatal(e)
	}
	duration := int64(1000)
	title := "test video"
	snap := domain.Snapshot{ID: domain.ID(), SourceID: c.Run.SourceID, VideoHash: "video-hash", TextHash: "text-hash", DurationMS: duration, Source: domain.Source{SourceID: c.Run.SourceID, Title: &title, DurationMS: &duration, TextProvenance: &domain.Provenance{Origin: "platform_subtitles", Coverage: "complete"}}, Metadata: []byte(`{}`)}
	_, e = s.FinishSource(ctx, *c, snap, []domain.Segment{{Ordinal: 0, StartMS: 0, EndMS: 1000, Text: "source", Origin: "platform_subtitles"}}, video.ID)
	if e != nil {
		t.Fatal(e)
	}
	r, e = s.Request(ctx, owner, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	c, e = s.Claim(ctx, *r.ActiveRunID, 1, "generation")
	if e != nil || c == nil {
		t.Fatal(e)
	}
	return r, *c
}
func TestPublicationAndQuestionOrderingAreAtomic(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	r, c := prepared(t, s, owner)
	a := domain.Article{SchemaVersion: 1, SnapshotID: *r.SnapshotID, Title: "read only", Sections: []domain.Section{}, Citations: map[string]domain.Citation{}, Limitations: []domain.Limitation{}, Quality: "standard"}
	if e := s.Publish(ctx, c, a); e != nil {
		t.Fatal(e)
	}
	if e := s.Publish(ctx, c, a); !errors.Is(e, domain.ErrFenced) {
		t.Fatalf("duplicate publication: %v", e)
	}
	if e := s.CancelRequest(ctx, owner, r.ID); e != nil {
		t.Fatal(e)
	}
	r, e := s.Request(ctx, owner, r.ID)
	if e != nil || r.Status != "completed" || r.ArticleID == nil {
		t.Fatal("late cancellation removed a committed article")
	}
	con, _, e := s.EnsureConversation(ctx, owner, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	accepted := make(chan Accepted, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := s.CreateTurn(ctx, owner, con.ID, domain.ID(), domain.QuestionInput{Question: "explain source"}, "")
			if e != nil {
				errs <- e
			} else {
				accepted <- v
			}
		}()
	}
	wg.Wait()
	if len(accepted) != 1 || len(errs) != 1 {
		t.Fatalf("double tab accepted=%d errors=%d", len(accepted), len(errs))
	}
	x := <-accepted
	turn, _ := s.Turn(ctx, owner, con.ID, x.ID)
	if e = s.CancelTurn(ctx, owner, con.ID, turn.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateTurn(ctx, owner, con.ID, "next", domain.QuestionInput{Question: "next", ExpectedLastTurnID: &turn.ID}, ""); e == nil {
		t.Fatal("new turn accepted during cancellation")
	}
	rr, _ := s.Run(ctx, turn.RunID)
	if e = s.Repair(ctx, rr.ID, rr.DispatchGeneration, "unknown", nil); e != nil {
		t.Fatal(e)
	}
	newTurn, e := s.CreateTurn(ctx, owner, con.ID, "next", domain.QuestionInput{Question: "next", ExpectedLastTurnID: &turn.ID}, "")
	if e != nil {
		t.Fatal(e)
	}
	re, e := s.CreateTurn(ctx, owner, con.ID, "next", domain.QuestionInput{Question: "next", ExpectedLastTurnID: &turn.ID}, "")
	if e != nil || !re.Replayed || re.ID != newTurn.ID {
		t.Fatal("network replay rejected after tail advance")
	}
}
func TestCancellationWinsBeforePublication(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	r, c := prepared(t, s, owner)
	if e := s.CancelRequest(ctx, owner, r.ID); e != nil {
		t.Fatal(e)
	}
	if e := s.Publish(ctx, c, domain.Article{SnapshotID: *r.SnapshotID}); !errors.Is(e, domain.ErrFenced) {
		t.Fatal("canceled generation published")
	}
	var n int
	_ = s.Pool.QueryRow(ctx, "SELECT count(*) FROM articles").Scan(&n)
	if n != 0 {
		t.Fatal("canceled result became visible")
	}
}
func TestPinsAndDeletionShareOneCommitOrder(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	_, c := prepared(t, s, owner)
	id := domain.ID()
	a := domain.Asset{ID: id, SourceID: c.Run.SourceID, SnapshotID: c.Run.SnapshotID, RunID: c.Run.ID, Kind: "frame", StorageKey: id + ".jpg", SHA256: "hash", MIME: "image/jpeg", Bytes: 3, TimeMS: domain.Ptr(int64(0)), RequestedTimeMS: domain.Ptr(int64(0)), ExpiresAt: domain.Ptr(time.Now().Add(-time.Hour))}
	if e := s.AddAsset(ctx, c, a); e != nil {
		t.Fatal(e)
	}
	assets, e := s.CleanupAssets(ctx, false)
	if e != nil || len(assets) != 0 {
		t.Fatal("active checkpoint image selected for deletion")
	}
	_, _ = s.Pool.Exec(ctx, "DELETE FROM asset_refs WHERE asset_id=$1", id)
	assets, e = s.CleanupAssets(ctx, false)
	if e != nil || len(assets) != 1 {
		t.Fatalf("expired image not marked deleting: %v", e)
	}
	if e = s.Pin(ctx, c, id); e == nil {
		t.Fatal("new pin resurrected a deleting asset")
	}
}
func TestDeliveryRepairDoesNotRefreshBudgets(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	a, e := s.CreateRequest(ctx, owner, "repair", "https://youtube.com/watch?v=abcdefghijk", "", false)
	if e != nil {
		t.Fatal(e)
	}
	r, _ := s.Request(ctx, owner, a.ID)
	run, _ := s.Run(ctx, *r.ActiveRunID)
	_, _ = s.Pool.Exec(ctx, "UPDATE task_outbox SET delivered_at=now() WHERE run_id=$1", run.ID)
	_, _ = s.Pool.Exec(ctx, "UPDATE job_runs SET failure_count=1,active_ms=1234 WHERE id=$1", run.ID)
	if e = s.Repair(ctx, run.ID, 1, "unknown", nil); e != nil {
		t.Fatal(e)
	}
	same, _ := s.Run(ctx, run.ID)
	if same.DispatchGeneration != 1 {
		t.Fatal("unknown broker state treated as missing")
	}
	if e = s.Repair(ctx, run.ID, 1, "missing", nil); e != nil {
		t.Fatal(e)
	}
	next, _ := s.Run(ctx, run.ID)
	if next.DispatchGeneration != 2 || next.FailureCount != 1 || next.ActiveMS != 1234 || !next.DeadlineAt.Equal(run.DeadlineAt) {
		t.Fatal("repair refreshed the budget or deadline")
	}
	if e = s.Repair(ctx, run.ID, 1, "missing", nil); e != nil {
		t.Fatal(e)
	}
	again, _ := s.Run(ctx, run.ID)
	if again.DispatchGeneration != 2 {
		t.Fatal("stale repair duplicated delivery")
	}
}
func TestAnswerImagesStayPrivateAndReadableAfterCommit(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner, other := session(t, s), session(t, s)
	r, c := prepared(t, s, owner)
	if e := s.Publish(ctx, c, domain.Article{SnapshotID: *r.SnapshotID, Sections: []domain.Section{}, Citations: map[string]domain.Citation{}, Limitations: []domain.Limitation{}}); e != nil {
		t.Fatal(e)
	}
	con, _, e := s.EnsureConversation(ctx, owner, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	accepted, e := s.CreateTurn(ctx, owner, con.ID, "image", domain.QuestionInput{Question: "inspect this video"}, "")
	if e != nil {
		t.Fatal(e)
	}
	turn, _ := s.Turn(ctx, owner, con.ID, accepted.ID)
	answerClaim, e := s.Claim(ctx, turn.RunID, 1, "answer")
	if e != nil || answerClaim == nil {
		t.Fatal(e)
	}
	id := domain.ID()
	a := domain.Asset{ID: id, SourceID: c.Run.SourceID, SnapshotID: r.SnapshotID, RunID: answerClaim.Run.ID, Kind: "frame", StorageKey: id + ".jpg", SHA256: "hash", MIME: "image/jpeg", Bytes: 3, TimeMS: domain.Ptr(int64(0)), RequestedTimeMS: domain.Ptr(int64(0)), ExpiresAt: domain.Ptr(time.Now().Add(time.Hour))}
	if e = s.AddAsset(ctx, *answerClaim, a); e != nil {
		t.Fatal(e)
	}
	if e = s.CompleteAnswer(ctx, *answerClaim, domain.Answer{Kind: "grounded", Frames: []domain.Frame{{ID: id}}}); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.Frame(ctx, owner, r.ID, id); e != nil {
		t.Fatal("own committed answer frame became unreadable:", e)
	}
	if _, _, e = s.Frame(ctx, other, r.ID, id); e == nil {
		t.Fatal("private answer image leaked across sessions")
	}
}
func TestRestoreWaitReusesTurnAndCancellationReleasesDemand(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	r, c := prepared(t, s, owner)
	if e := s.Publish(ctx, c, domain.Article{SnapshotID: *r.SnapshotID, Sections: []domain.Section{}, Citations: map[string]domain.Citation{}, Limitations: []domain.Limitation{}}); e != nil {
		t.Fatal(e)
	}
	con, _, _ := s.EnsureConversation(ctx, owner, r.ID)
	a, e := s.CreateTurn(ctx, owner, con.ID, "restore", domain.QuestionInput{Question: "show a new frame"}, "")
	if e != nil {
		t.Fatal(e)
	}
	turn, _ := s.Turn(ctx, owner, con.ID, a.ID)
	claim, e := s.Claim(ctx, turn.RunID, 1, "answer")
	if e != nil || claim == nil {
		t.Fatal(e)
	}
	if e = s.Checkpoint(ctx, *claim, map[string]any{"schema_version": 1, "pending": "inspection"}); e != nil {
		t.Fatal(e)
	}
	if e = s.WaitForRestore(ctx, *claim); e != nil {
		t.Fatal(e)
	}
	waiting, _ := s.Run(ctx, turn.RunID)
	if waiting.Status != "waiting_dependency" || waiting.DependencyRunID == nil || !waiting.RestoreUsed || waiting.LeaseOwner != nil {
		t.Fatal("answer did not release slot for restore")
	}
	if waiting.DeadlineAt.After(waiting.CreatedAt.Add(30*time.Minute + time.Second)) {
		t.Fatal("restore reset the original question time")
	}
	if e = s.CancelTurn(ctx, owner, con.ID, turn.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.Repair(ctx, turn.RunID, 1, "unknown", nil); e != nil {
		t.Fatal(e)
	}
	dep, _ := s.Run(ctx, *waiting.DependencyRunID)
	if dep.Status != "cancel_requested" {
		t.Fatal("last canceled answer left an unneeded restore running")
	}
	after, _ := s.Run(ctx, turn.RunID)
	if after.Status != "canceled" || string(after.Checkpoint) != string(waiting.Checkpoint) {
		t.Fatal("cancellation corrupted the checkpoint")
	}
}
