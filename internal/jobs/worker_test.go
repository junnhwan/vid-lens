package jobs

import (
	"context"
	"testing"
	"time"

	"vidlens/internal/domain"
	"vidlens/internal/investigator"
	"vidlens/internal/media"
	"vidlens/internal/testsupport"
)

func TestRealBrokerOutboxReplayAndLostDeliveryRepair(t *testing.T) {
	s, rm, cfg := testsupport.Services(t)
	ctx := context.Background()
	ms, e := media.New(s, rm, cfg)
	if e != nil {
		t.Fatal(e)
	}
	engine := &investigator.Engine{Store: s, Media: ms, Resources: rm, Config: cfg}
	w, e := New(s, ms, engine, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer w.client.Close()
	defer w.inspector.Close()
	owner, e := s.NewSession(ctx, domain.ID(), time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	a, e := s.CreateRequest(ctx, owner.ID, "dispatch", "local://absent.mp4", "", true)
	if e != nil {
		t.Fatal(e)
	}
	r, _ := s.Request(ctx, owner.ID, a.ID)
	run, _ := s.Run(ctx, *r.ActiveRunID)
	w.dispatch(ctx)
	first, e := w.inspector.GetTaskInfo("generation", taskID(run.ID, 1))
	if e != nil {
		t.Fatal(e)
	}
	if first.MaxRetry != 25 || first.Deadline.IsZero() || first.Timeout != 45*time.Minute+15*time.Second {
		t.Fatal("queue used implicit retry or timeout defaults")
	}
	_, e = s.Pool.Exec(ctx, "UPDATE task_outbox SET delivered_at=NULL,claimed_until=NULL,not_before=now() WHERE run_id=$1", run.ID)
	if e != nil {
		t.Fatal(e)
	}
	w.dispatch(ctx)
	var delivered bool
	if e = s.Pool.QueryRow(ctx, "SELECT delivered_at IS NOT NULL FROM task_outbox WHERE run_id=$1 AND dispatch_generation=1", run.ID).Scan(&delivered); e != nil || !delivered {
		t.Fatal("uncertain enqueue did not recover by matching task identity")
	}
	if e = w.inspector.DeleteTask("generation", taskID(run.ID, 1)); e != nil {
		t.Fatal(e)
	}
	w.reconcile(ctx)
	repaired, _ := s.Run(ctx, run.ID)
	if repaired.DispatchGeneration != 2 || repaired.FailureCount != 0 || !repaired.DeadlineAt.Equal(run.DeadlineAt) {
		t.Fatal("lost delivery did not preserve business budget")
	}
	w.dispatch(ctx)
	if _, e = w.inspector.GetTaskInfo("generation", taskID(run.ID, 2)); e != nil {
		t.Fatal(e)
	}
	_ = w.inspector.DeleteTask("generation", taskID(run.ID, 2))
}
func TestWorkerCommitsAcceptedFailureAndAcknowledgesQueue(t *testing.T) {
	s, rm, cfg := testsupport.Services(t)
	ctx, cancel := context.WithCancel(context.Background())
	ms, e := media.New(s, rm, cfg)
	if e != nil {
		t.Fatal(e)
	}
	w, e := New(s, ms, &investigator.Engine{Store: s, Media: ms, Resources: rm, Config: cfg}, cfg)
	if e != nil {
		t.Fatal(e)
	}
	if e = w.Start(ctx); e != nil {
		t.Fatal(e)
	}
	defer func() { cancel(); w.Shutdown() }()
	owner, _ := s.NewSession(ctx, domain.ID(), time.Hour)
	a, e := s.CreateRequest(ctx, owner.ID, "accepted-failure", "local://missing.mp4", "", true)
	if e != nil {
		t.Fatal(e)
	}
	end := time.Now().Add(15 * time.Second)
	for time.Now().Before(end) {
		r, e := s.Request(ctx, owner.ID, a.ID)
		if e != nil {
			t.Fatal(e)
		}
		if r.Status == "failed" {
			if r.Failure == nil || r.ArticleID != nil {
				t.Fatal("queue failure became fake success")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("accepted failure did not become a durable terminal state")
}

func TestMalformedResultAllowsExplicitRetryWithoutAutomaticResubmission(t *testing.T) {
	failure, automatic, _ := classify(domain.Err(502, "REVIEW_RESULT_INVALID", "reviewer returned an invalid report"))
	if !failure.CanRetry || automatic {
		t.Fatal("review format failure must allow explicit retry without automatic resubmission", failure, automatic)
	}
	for _, code := range []string{"MODEL_RESULT_INVALID", "MODEL_RESULT_NOT_SUBMITTED", "MODEL_OUTPUT_TRUNCATED"} {
		failure, automatic, _ := classify(domain.Err(422, code, "invalid structured result"))
		if !failure.CanRetry || automatic {
			t.Fatalf("wrong retry semantics for %s: %+v auto=%v", code, failure, automatic)
		}
	}
}
