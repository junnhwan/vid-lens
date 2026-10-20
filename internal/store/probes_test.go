package store

import (
	"context"
	"testing"
	"time"

	"vidlens/internal/domain"
)

func TestProbesClaimDifferentModelsConcurrently(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a, b := enabledModel(t, s, "parallel-a"), enabledModel(t, s, "parallel-b")
	first, err := s.CreateProbe(ctx, a.ID, a.RevisionID, "capabilities", domain.ID())
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateProbe(ctx, b.ID, b.RevisionID, "capabilities", domain.ID())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{first.ID, second.ID} {
		p, err := s.ClaimProbe(ctx, id)
		if err != nil || p == nil {
			t.Fatalf("different models must run concurrently: probe=%s err=%v", id, err)
		}
	}
}

func TestProbeAdmissionRejectsDuplicateModelButReplaysOriginal(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	m := enabledModel(t, s, "duplicate")
	a, err := s.CreateProbe(ctx, m.ID, m.RevisionID, "capabilities", "same-request")
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := s.CreateProbe(ctx, m.ID, m.RevisionID, "capabilities", "same-request"); err != nil || replay.ID != a.ID || !replay.Replayed {
		t.Fatalf("idempotent replay: %+v %v", replay, err)
	}
	if _, err := s.CreateProbe(ctx, m.ID, m.RevisionID, "cache", "another-request"); err == nil {
		t.Fatal("same model accepted a second paid check while the first is open")
	}
}

func TestProbeClaimReceivesFullExecutionBudget(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	m := enabledModel(t, s, "execution-clock")
	a, err := s.CreateProbe(ctx, m.ID, m.RevisionID, "capabilities", domain.ID())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, "UPDATE model_probes SET created_at=now()-interval '178 seconds',deadline_at=now()+interval '2 seconds' WHERE id=$1", a.ID); err != nil {
		t.Fatal(err)
	}
	p, err := s.ClaimProbe(ctx, a.ID)
	if err != nil || p == nil {
		t.Fatalf("claim: %v", err)
	}
	if time.Until(p.DeadlineAt) < 4*time.Minute {
		t.Fatalf("queue time consumed execution budget: remaining=%s", time.Until(p.DeadlineAt))
	}
}

func TestProbeTimeoutSettlesWithoutRepeatingSubmittedCalls(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	m := enabledModel(t, s, "timeout")
	a, err := s.CreateProbe(ctx, m.ID, m.RevisionID, "capabilities", domain.ID())
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.ClaimProbe(ctx, a.ID)
	if err != nil || p == nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err = s.BeginProbeSubmission(ctx, *p); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, "UPDATE model_probes SET deadline_at=now()-interval '1 second' WHERE id=$1", p.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.FinishProbe(ctx, *p, nil, "PROBE_EXECUTION_TIMEOUT"); err != nil {
		t.Fatalf("expired execution could not settle: %v", err)
	}
	checks, err := s.Probes(ctx, m.ID)
	if err != nil || checks[0].Status != "unknown" || checks[0].Submissions != 1 {
		t.Fatalf("submitted timeout must stay unknown: %+v %v", checks, err)
	}
	if p, err = s.ClaimProbe(ctx, a.ID); err != nil || p != nil {
		t.Fatal("settled request was submitted again")
	}
}
