package jobs

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hibiken/asynq"
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

func TestReviewRecoveryRequiresExplicitRetry(t *testing.T) {
	for _, code := range []string{"REVIEW_COVERAGE_INCOMPLETE", "REVIEW_BUDGET_EXHAUSTED", "REVIEW_INPUT_TOO_LARGE"} {
		t.Run(code, func(t *testing.T) {
			for _, status := range []int{422, 502} {
				failure, automatic, after := classify(domain.Err(status, code, "review draft is retained"))
				if !failure.CanRetry || automatic || after != 0 {
					t.Fatalf("review failure must retain explicit retry without automatic resubmission: %+v auto=%t after=%s", failure, automatic, after)
				}
			}
			failure, automatic, _ := classify(&domain.Problem{Status: 422, Code: code, Detail: "explicitly denied", Meta: map[string]any{"can_retry": false}})
			if failure.CanRetry || automatic {
				t.Fatal("failure override must remain authoritative", failure, automatic)
			}
		})
	}
	for _, code := range []string{"REVIEW_UNKNOWN_FAILURE", "REVIEW_CONTRACT_UNSUPPORTED"} {
		failure, automatic, _ := classify(domain.Err(422, code, "unknown review state"))
		if failure.CanRetry || automatic {
			t.Fatal("unknown review codes must not be made retryable by prefix", failure, automatic)
		}
	}
}

func TestReviewRetryabilityDoesNotResumeFailedOrCanceledRun(t *testing.T) {
	for _, mode := range []string{"failed", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			db, _, _ := testsupport.Services(t)
			ctx := context.Background()
			turn, claim := testsupport.Question(t, db)
			checkpoint := map[string]any{"schema_version": 1, "review_draft": "retained"}
			if err := db.Checkpoint(ctx, claim, checkpoint); err != nil {
				t.Fatal(err)
			}
			if mode == "canceled" {
				var owner string
				if err := db.Pool.QueryRow(ctx, "SELECT session_id FROM qa_conversations WHERE id=$1", turn.ConversationID).Scan(&owner); err != nil {
					t.Fatal(err)
				}
				if err := db.CancelTurn(ctx, owner, turn.ConversationID, turn.ID); err != nil {
					t.Fatal(err)
				}
			}
			failure, automatic, _ := classify(domain.Err(422, "REVIEW_COVERAGE_INCOMPLETE", "review draft retained"))
			if !failure.CanRetry || automatic {
				t.Fatal("explicit retry classification lost", failure, automatic)
			}
			retry, err := db.Fail(ctx, claim, failure, automatic, 0)
			if err != nil || retry {
				t.Fatal("review failure scheduled automatic recovery", retry, err)
			}
			// A duplicate queue delivery must stop at the terminal claim check.
			// The absent Agent would fail immediately if work resumed here.
			worker := &Worker{Store: db, ctx: ctx, owner: "terminal-replay"}
			task := asynq.NewTask("vidlens.run", domain.JSON(payload{SchemaVersion: 1, RunID: claim.Run.ID, Generation: claim.Run.DispatchGeneration}))
			if err := worker.handle(ctx, task); err != nil {
				t.Fatal("terminal delivery was not acknowledged", err)
			}
			after, err := db.Run(ctx, claim.Run.ID)
			if err != nil || after.Status != mode || after.DispatchGeneration != claim.Run.DispatchGeneration {
				t.Fatal("review retryability revived a terminal run", after.Status, err)
			}
			if mode == "canceled" && after.Failure != nil {
				t.Fatal("review failure replaced the cancellation", after.Failure)
			}
			var saved map[string]any
			if err := json.Unmarshal(after.Checkpoint, &saved); err != nil || saved["review_draft"] != "retained" {
				t.Fatal("terminal review lost the saved draft checkpoint", err)
			}
			var submissions int
			if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM model_submissions WHERE run_id=$1", claim.Run.ID).Scan(&submissions); err != nil || submissions != 0 {
				t.Fatal("terminal replay submitted a model request", submissions, err)
			}
		})
	}
}
