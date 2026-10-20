package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
)

func waitClaims(t *testing.T, s *Store) (domain.Claim, domain.Claim) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.SaveExecutionSettings(ctx, ExecutionInput{MaxVideoRunning: 2}); err != nil {
		t.Fatal(err)
	}
	owner := session(t, s)
	claims := []domain.Claim{}
	for _, url := range []string{"local://holder.mp4", "local://waiter.mp4"} {
		a, err := s.CreateRequest(ctx, owner, domain.ID(), url, "", true)
		if err != nil {
			t.Fatal(err)
		}
		r, err := s.Request(ctx, owner, a.ID)
		if err != nil {
			t.Fatal(err)
		}
		c, err := s.Claim(ctx, *r.ActiveRunID, 1, domain.ID())
		if err != nil || c == nil {
			t.Fatal(err)
		}
		claims = append(claims, *c)
	}
	return claims[0], claims[1]
}

func TestResourceWaitRequiresALiveMatchingHolder(t *testing.T) {
	for _, kind := range []string{"live", "missing", "other_resource", "expired_resource", "expired_run", "old_epoch"} {
		t.Run(kind, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			holder, waiter := waitClaims(t, s)
			resource := "download"
			if kind == "other_resource" {
				resource = "ffmpeg"
			}
			if kind != "missing" {
				if ok, err := s.AcquireResource(ctx, holder, resource, "holder", 1); err != nil || !ok {
					t.Fatal(ok, err)
				}
			}
			queries := map[string]string{
				"expired_resource": "UPDATE resource_leases SET expires_at=now()-interval '1 second' WHERE run_id=$1",
				"expired_run":      "UPDATE job_runs SET lease_expires_at=now()-interval '1 second' WHERE id=$1",
				"old_epoch":        "UPDATE job_runs SET execution_epoch=execution_epoch+1 WHERE id=$1",
			}
			if query := queries[kind]; query != "" {
				if _, err := s.Pool.Exec(ctx, query, holder.Run.ID); err != nil {
					t.Fatal(err)
				}
			}
			logged, err := s.RecordResourceWait(ctx, waiter, "waiter", "download", 1)
			if err != nil || logged != (kind == "live") {
				t.Fatal("unverified resource wait accepted", logged, err)
			}
		})
	}
}

func TestResourceWaitCompletedGraceExpiresAndNeverChangesBusinessProgress(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	holder, waiter := waitClaims(t, s)
	if ok, err := s.AcquireResource(ctx, holder, "download", "holder", 1); err != nil || !ok {
		t.Fatal(err)
	}
	if ok, err := s.RecordResourceWait(ctx, waiter, "waiter", "download", 1); err != nil || !ok {
		t.Fatal(err)
	}
	for _, query := range []string{"UPDATE job_runs SET updated_at=now()-interval '6 minutes' WHERE id=$1", "UPDATE run_operations SET started_at=now()-interval '6 minutes' WHERE run_id=$1 AND kind='resource_wait'"} {
		if _, err := s.Pool.Exec(ctx, query, waiter.Run.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.EndResourceWait(ctx, waiter, "waiter"); err != nil {
		t.Fatal(err)
	}
	if err := s.Repair(ctx, waiter.Run.ID, 1, "active", nil); err != nil {
		t.Fatal(err)
	}
	run, err := s.Run(ctx, waiter.Run.ID)
	if err != nil || run.Status != "running" || time.Since(run.UpdatedAt) < 5*time.Minute {
		t.Fatal("wait completion fabricated progress or lost its bounded credit", run, err)
	}
	for _, query := range []string{"UPDATE job_runs SET updated_at=now()-interval '12 minutes' WHERE id=$1", "UPDATE run_operations SET started_at=now()-interval '12 minutes',completed_at=now()-interval '6 minutes' WHERE run_id=$1 AND kind='resource_wait'"} {
		if _, err := s.Pool.Exec(ctx, query, waiter.Run.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Repair(ctx, waiter.Run.ID, 1, "active", nil); err != nil {
		t.Fatal(err)
	}
	run, err = s.Run(ctx, waiter.Run.ID)
	if err != nil || run.Status != "failed" || run.Failure == nil || run.Failure.Message != "处理长时间没有业务进展。" {
		t.Fatal("completed resource wait hid a later stalled operation", run, err)
	}
}

func TestResourceWaitCreditsUnionOfVerifiedIntervals(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	holder, waiter := waitClaims(t, s)
	if ok, err := s.AcquireResource(ctx, holder, "download", "holder", 1); err != nil || !ok {
		t.Fatal(err)
	}
	for _, owner := range []string{"one", "two"} {
		if ok, err := s.RecordResourceWait(ctx, waiter, owner, "download", 1); err != nil || !ok {
			t.Fatal(err)
		}
	}
	if _, err := s.Pool.Exec(ctx, "UPDATE job_runs SET updated_at=now()-interval '12 minutes' WHERE id=$1", waiter.Run.ID); err != nil {
		t.Fatal(err)
	}
	for i, owner := range []string{"one", "two"} {
		if _, err := s.Pool.Exec(ctx, `UPDATE run_operations SET started_at=now()-make_interval(mins=>$3),completed_at=now()-make_interval(mins=>$4),status='completed' WHERE run_id=$1 AND operation_key=$2`, waiter.Run.ID, "resource-wait:"+owner, 12-i*2, 4-i*2); err != nil {
			t.Fatal(err)
		}
	}
	run, err := s.Run(ctx, waiter.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var credit time.Duration
	if err := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		credit, err = resourceWaitDuration(ctx, tx, run, s.DBTime(ctx))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if credit < 10*time.Minute-time.Second || credit > 10*time.Minute+time.Second {
		t.Fatal("parallel waits were counted more than once", credit)
	}
}

func TestResourceWaitStaleVerificationAndOldAttemptCannotHideAStall(t *testing.T) {
	for _, kind := range []string{"stale_verification", "old_attempt"} {
		t.Run(kind, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			holder, waiter := waitClaims(t, s)
			if ok, err := s.AcquireResource(ctx, holder, "download", "holder", 1); err != nil || !ok {
				t.Fatal(err)
			}
			if ok, err := s.RecordResourceWait(ctx, waiter, "waiter", "download", 1); err != nil || !ok {
				t.Fatal(err)
			}
			if _, err := s.Pool.Exec(ctx, "UPDATE job_runs SET updated_at=now()-interval '12 minutes' WHERE id=$1", waiter.Run.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Pool.Exec(ctx, "UPDATE run_operations SET started_at=now()-interval '12 minutes',result=jsonb_set(result,'{verified_until}',to_jsonb(now()-interval '6 minutes')) WHERE run_id=$1 AND kind='resource_wait'", waiter.Run.ID); err != nil {
				t.Fatal(err)
			}
			if kind == "old_attempt" {
				if _, err := s.Pool.Exec(ctx, "UPDATE job_runs SET execution_epoch=execution_epoch+1 WHERE id=$1", waiter.Run.ID); err != nil {
					t.Fatal(err)
				}
				if err := s.EndResourceWait(ctx, waiter, "waiter"); !errors.Is(err, domain.ErrFenced) {
					t.Fatal("old worker ended the new execution's wait", err)
				}
			}
			if err := s.Repair(ctx, waiter.Run.ID, 1, "active", nil); err != nil {
				t.Fatal(err)
			}
			run, err := s.Run(ctx, waiter.Run.ID)
			if err != nil || run.Status != "failed" || run.Failure == nil || run.Failure.Code != "INTERNAL_FAILURE" {
				t.Fatal("stale resource wait suppressed the watchdog", run, err)
			}
		})
	}
}

func TestResourceWaitEnforcesItsFrozenQueueLimit(t *testing.T) {
	for _, action := range []string{"record", "end", "repair"} {
		t.Run(action, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			holder, waiter := waitClaims(t, s)
			if ok, err := s.AcquireResource(ctx, holder, "download", "holder", 1); err != nil || !ok {
				t.Fatal(err)
			}
			if ok, err := s.RecordResourceWait(ctx, waiter, "waiter", "download", 1); err != nil || !ok {
				t.Fatal(err)
			}
			if _, err := s.Pool.Exec(ctx, "UPDATE run_operations SET started_at=now()-interval '21 minutes',result=jsonb_set(result,'{limit_ms}','1200000') WHERE run_id=$1 AND kind='resource_wait'", waiter.Run.ID); err != nil {
				t.Fatal(err)
			}
			var err error
			switch action {
			case "record":
				_, err = s.RecordResourceWait(ctx, waiter, "waiter", "download", 1)
			case "end":
				err = s.EndResourceWait(ctx, waiter, "waiter")
			case "repair":
				err = s.Repair(ctx, waiter.Run.ID, 1, "active", nil)
				if err != nil {
					t.Fatal(err)
				}
				run, err := s.Run(ctx, waiter.Run.ID)
				if err != nil || run.Failure == nil || run.Failure.Code != "QUEUE_WAIT_TIMEOUT" {
					t.Fatal("expired wait did not report queue timeout", run, err)
				}
				return
			}
			var problem *domain.Problem
			if !errors.As(err, &problem) || problem.Code != "QUEUE_WAIT_TIMEOUT" {
				t.Fatal("expired wait was silently accepted", err)
			}
		})
	}
}
