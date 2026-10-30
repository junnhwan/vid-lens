package jobs

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"vidlens/internal/domain"
	"vidlens/internal/investigator"
	"vidlens/internal/media"
	"vidlens/internal/testsupport"
)

func TestQuestionDeliveryUsesFrozenRoundTimeout(t *testing.T) {
	db, _, cfg := testsupport.Services(t)
	ctx := context.Background()
	_, claim := testsupport.Question(t, db)
	w, err := New(db, nil, nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.client.Close()
	defer w.inspector.Close()
	w.dispatch(ctx)
	id := taskID(claim.Run.ID, claim.Run.DispatchGeneration)
	task, err := w.inspector.GetTaskInfo("interactive", id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := w.inspector.DeleteTask("interactive", id); err != nil {
			t.Error(err)
		}
		if err := w.inspector.DeleteQueue("interactive", false); err != nil {
			t.Error(err)
		}
	}()
	if task.Timeout != 20*time.Minute+15*time.Second || !task.Deadline.Equal(claim.Run.DeadlineAt.Truncate(time.Second)) {
		t.Fatal("broker kept a shorter deadline", task.Timeout, task.Deadline, claim.Run.DeadlineAt)
	}
}

func TestQuestionStopCancelsRealReasoningStreamAndNeverPublishes(t *testing.T) {
	db, resources, cfg := testsupport.Services(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	started, closed := make(chan struct{}), make(chan struct{})
	var first sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		first.Do(func() { close(started) })
		defer close(closed)
		for {
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"private-working\"}}]}\n\n")
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	}))
	defer server.Close()
	cfg.Profile.ModelBaseURL = server.URL
	if _, err := db.SaveConfig(ctx, cfg.Profile, "test", ""); err != nil {
		t.Fatal(err)
	}
	turn, claim := testsupport.Question(t, db)
	if err := db.Suspend(ctx, claim); err != nil {
		t.Fatal(err)
	}
	ms, err := media.New(db, resources, cfg)
	if err != nil {
		t.Fatal(err)
	}
	w := &Worker{Store: db, Media: ms, Agent: &investigator.Engine{Store: db, Resources: resources, Config: cfg}, Config: cfg, ctx: ctx, owner: "question-cancel-fixture"}
	done := make(chan error, 1)
	go func() {
		done <- w.handle(ctx, asynq.NewTask("vidlens.run", domain.JSON(payload{1, claim.Run.ID, claim.Run.DispatchGeneration})))
	}()
	select {
	case <-started:
	case err := <-done:
		t.Fatal("worker stopped before model stream", err)
	case <-ctx.Done():
		t.Fatal("model did not start")
	}
	var thinking bool
	for until := time.Now().Add(2 * time.Second); time.Now().Before(until); {
		if err = db.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM run_events WHERE run_id=$1 AND step->'model_progress'->>'state'='thinking')", claim.Run.ID).Scan(&thinking); err != nil {
			t.Fatal(err)
		}
		if thinking {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !thinking {
		t.Fatal("real reasoning never reached public activity metadata")
	}
	var owner string
	if err = db.Pool.QueryRow(ctx, "SELECT session_id FROM qa_conversations WHERE id=$1", turn.ConversationID).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if err = db.CancelTurn(ctx, owner, turn.ConversationID, turn.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("cancel ignored by reasoning stream")
	}
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal("provider request remained open")
	}
	after, err := db.Turn(ctx, owner, turn.ConversationID, turn.ID)
	if err != nil || after.Status != "canceled" || after.Answer != nil {
		t.Fatal("canceled work published or did not settle", after.Status, err)
	}
	var leaked bool
	if err = db.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM run_events WHERE run_id=$1 AND strpos(step::text,'private-working')>0)", claim.Run.ID).Scan(&leaked); err != nil || leaked {
		t.Fatal("private reasoning appeared in public progress", err)
	}
}
