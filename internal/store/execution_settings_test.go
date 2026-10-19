package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"vidlens/internal/domain"
)

func queuedExecution(t *testing.T, s *Store, kind string) string {
	t.Helper()
	ctx := context.Background()
	a, e := s.CreateRequest(ctx, session(t, s), domain.ID(), "local://"+domain.ID()+".mp4", "", true)
	if e != nil {
		t.Fatal(e)
	}
	// No media or provider is exercised by these scheduler tests.
	var id string
	if e = s.Pool.QueryRow(ctx, "SELECT active_run_id FROM generation_requests WHERE id=$1", a.ID).Scan(&id); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, "UPDATE job_runs SET kind=$2 WHERE id=$1", id, kind); e != nil {
		t.Fatal(e)
	}
	return id
}

func TestExecutionSettingsApplyAcrossStoresAndDrainWithoutInterrupting(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	s.VideoConcurrency = 1
	v, e := s.ExecutionSettings(ctx)
	if e != nil || v.Revision != 0 || v.MaxVideoRunning != 1 {
		t.Fatalf("initial: %+v %v", v, e)
	}
	v, e = s.SaveExecutionSettings(ctx, ExecutionInput{MaxVideoRunning: 10})
	if e != nil {
		t.Fatal(e)
	}
	// Another store instance has a different startup fallback. Persisted values
	// must govern both workers and survive process reconstruction.
	other := &Store{Pool: s.Pool, VideoConcurrency: 64}
	got, e := other.ExecutionSettings(ctx)
	if e != nil || got != v {
		t.Fatalf("persisted setting: %+v %v", got, e)
	}
	ids := make([]string, 12)
	for i := range ids {
		ids[i] = queuedExecution(t, s, "article.generate")
	}
	var mu sync.Mutex
	var claims []domain.Claim
	var waiting []string
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			worker := s
			if i%2 == 0 {
				worker = other
			}
			c, e := worker.Claim(ctx, id, 1, fmt.Sprint("worker-", i))
			mu.Lock()
			defer mu.Unlock()
			if errors.Is(e, ErrExecutionBusy) {
				waiting = append(waiting, id)
			} else if e != nil || c == nil {
				t.Errorf("claim: %v", e)
			} else {
				claims = append(claims, *c)
			}
		}(i, id)
	}
	wg.Wait()
	if len(claims) != 10 || len(waiting) != 2 {
		t.Fatalf("claimed=%d waiting=%d", len(claims), len(waiting))
	}
	v, e = other.SaveExecutionSettings(ctx, ExecutionInput{ExpectedRevision: v.Revision, MaxVideoRunning: 2})
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range claims {
		if e = s.Check(ctx, c); e != nil {
			t.Fatalf("lowering interrupted an execution: %v", e)
		}
	}
	q, e := s.QueueStatus(ctx)
	if e != nil || q.MaxConcurrent != 2 || q.Processing != 10 || q.Waiting != 2 {
		t.Fatalf("draining snapshot: %+v %v", q, e)
	}
	for n := 0; n < 3; n++ {
		if _, e = s.Claim(ctx, waiting[0], 1, "waiting"); !errors.Is(e, ErrExecutionBusy) {
			t.Fatalf("admitted above lowered limit: %v", e)
		}
	}
	r, _ := s.Run(ctx, waiting[0])
	if r.AttemptNo != 0 || r.FailureCount != 0 || r.ActiveMS != 0 || r.Status != "queued" {
		t.Fatal("capacity wait spent business budget")
	}
	finish := func(c domain.Claim) {
		t.Helper()
		if _, e = s.Fail(ctx, c, domain.Failure{Code: "TEST_DONE", Message: "scheduler test"}, false, 0); e != nil {
			t.Fatal(e)
		}
	}
	for _, c := range claims[:8] {
		finish(c)
	}
	if _, e = s.Claim(ctx, waiting[0], 1, "waiting"); !errors.Is(e, ErrExecutionBusy) {
		t.Fatal("started while two old executions still run")
	}
	finish(claims[8])
	if c, e := other.Claim(ctx, waiting[0], 1, "next"); e != nil || c == nil {
		t.Fatalf("did not resume below new limit: %v", e)
	}
	if _, e = s.SaveExecutionSettings(ctx, ExecutionInput{ExpectedRevision: v.Revision - 1, MaxVideoRunning: 10}); e == nil {
		t.Fatal("stale settings overwrite accepted")
	}
	for _, n := range []int{0, -1, 65} {
		if _, e = s.SaveExecutionSettings(ctx, ExecutionInput{ExpectedRevision: v.Revision, MaxVideoRunning: n}); e == nil {
			t.Fatalf("invalid limit %d accepted", n)
		}
	}
}

func TestSourcePreparationHasItsOwnBoundAndWaitingDoesNotReserveDisk(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, e := s.SaveExecutionSettings(ctx, ExecutionInput{MaxVideoRunning: 10}); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		id := queuedExecution(t, s, "source.prepare")
		if c, e := s.Claim(ctx, id, 1, "source"); e != nil || c == nil {
			t.Fatalf("source %d: %v", i, e)
		}
	}
	id := queuedExecution(t, s, "source.prepare")
	if _, e := s.Claim(ctx, id, 1, "source"); !errors.Is(e, ErrExecutionBusy) {
		t.Fatalf("third source admitted: %v", e)
	}
	var n int
	if e := s.Pool.QueryRow(ctx, "SELECT count(*) FROM storage_reservations WHERE run_id=$1", id).Scan(&n); e != nil || n != 0 {
		t.Fatal("waiting source reserved disk")
	}
	article := queuedExecution(t, s, "article.generate")
	if c, e := s.Claim(ctx, article, 1, "article"); e != nil || c == nil {
		t.Fatalf("source preparation blocked article: %v", e)
	}
}

func TestExecutionMigrationAppendsAfterPublishedSettings(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	announcement, e := s.ApplyAnnouncement(ctx, AnnouncementCommand{Action: "publish", Title: "preserved notice", Body: "keep this content"})
	if e != nil {
		t.Fatal(e)
	}
	// Committed recommendation and announcement settings occupy registry
	// versions 18 and 19, regardless of their SQL filename prefixes.
	if _, e = s.Pool.Exec(ctx, "DROP TABLE execution_settings; DELETE FROM schema_migrations WHERE version=20"); e != nil {
		t.Fatal(e)
	}
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	if got, e := s.Announcements(ctx); e != nil || got.Revision != announcement.Revision || len(got.Items) != 1 || got.Items[0].ID != announcement.Items[0].ID || got.Items[0].Title != "preserved notice" || got.Items[0].Body != "keep this content" || got.Items[0].Version != announcement.Items[0].Version {
		t.Fatalf("existing announcement changed: %+v %v", got, e)
	}
	if _, e = s.SaveExecutionSettings(ctx, ExecutionInput{MaxVideoRunning: 10}); e != nil {
		t.Fatal(e)
	}
}
