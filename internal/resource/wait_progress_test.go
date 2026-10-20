package resource_test

import (
	"context"
	"testing"
	"time"

	"vidlens/internal/domain"
	"vidlens/internal/store"
	"vidlens/internal/testsupport"
)

func TestDownloadWaitIsNotBusinessStall(t *testing.T) {
	s, manager, _ := testsupport.Services(t)
	ctx := context.Background()
	if _, err := s.SaveExecutionSettings(ctx, store.ExecutionInput{MaxVideoRunning: 2}); err != nil {
		t.Fatal(err)
	}
	owner, err := s.NewSession(ctx, domain.ID(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	claims := []domain.Claim{}
	for _, url := range []string{"local://holder.mp4", "local://waiter.mp4"} {
		accepted, err := s.CreateRequest(ctx, owner.ID, domain.ID(), url, "", true)
		if err != nil {
			t.Fatal(err)
		}
		request, err := s.Request(ctx, owner.ID, accepted.ID)
		if err != nil {
			t.Fatal(err)
		}
		claim, err := s.Claim(ctx, *request.ActiveRunID, 1, domain.ID())
		if err != nil || claim == nil {
			t.Fatal(err)
		}
		claims = append(claims, *claim)
	}
	_, release, err := manager.Acquire(ctx, claims[0], "download")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	wait, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		_, release, err := manager.Acquire(wait, claims[1], "download")
		if err == nil {
			release()
		}
		done <- err
	}()
	<-started
	// The real resource holder is live; advance only the waiting business
	// clock and its matching wait record, without sleeping five minutes.
	deadline := time.Now().Add(3 * time.Second)
	for {
		var recorded bool
		if err := s.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM run_operations WHERE run_id=$1 AND kind='resource_wait' AND status='planned')", claims[1].Run.ID).Scan(&recorded); err != nil {
			t.Fatal(err)
		}
		if recorded {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("resource waiter did not record confirmed contention")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, query := range []string{"UPDATE job_runs SET updated_at=now()-interval '6 minutes' WHERE id=$1", "UPDATE run_operations SET started_at=now()-interval '6 minutes' WHERE run_id=$1 AND kind='resource_wait' AND status='planned'"} {
		if _, err := s.Pool.Exec(ctx, query, claims[1].Run.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Repair(ctx, claims[1].Run.ID, 1, "active", nil); err != nil {
		t.Fatal(err)
	}
	run, err := s.Run(ctx, claims[1].Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "running" {
		t.Fatalf("legitimate download waiter was killed as business stall: status=%s failure=%+v", run.Status, run.Failure)
	}
	if time.Since(run.UpdatedAt) < 5*time.Minute {
		t.Fatal("resource waiting fabricated business progress")
	}
	cancel()
	<-done
	var completed bool
	if err := s.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM run_operations WHERE run_id=$1 AND kind='resource_wait' AND status='completed')", claims[1].Run.ID).Scan(&completed); err != nil || !completed {
		t.Fatal("canceled waiter left an open resource wait", completed, err)
	}
}
