package store

import (
	"context"
	"testing"

	"vidlens/internal/domain"
)

func TestFinalFailureChargesTheLastHeartbeatIntervalOnce(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	_, claim := prepared(t, s, session(t, s))
	if _, err := s.Pool.Exec(ctx, "UPDATE job_attempts SET heartbeat_at=clock_timestamp()-interval '10 seconds' WHERE id=$1", claim.AttemptID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, "UPDATE job_runs SET active_ms=0 WHERE id=$1", claim.Run.ID); err != nil {
		t.Fatal(err)
	}
	retry, err := s.Fail(ctx, claim, domain.Failure{Code: "TASK_DEADLINE_EXCEEDED", CanRetry: true}, false, 0)
	if err != nil || retry {
		t.Fatal(retry, err)
	}
	run, err := s.Run(ctx, claim.Run.ID)
	if err != nil || run.ActiveMS < 10000 || run.ActiveMS > 13000 {
		t.Fatal("a ten-second heartbeat interval was counted more than once", run.ActiveMS, err)
	}
}
