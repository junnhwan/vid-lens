package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"vidlens/internal/domain"
	"vidlens/internal/store"
	"vidlens/internal/testsupport"
)

func TestLiveVideoLimitChangesWithoutRestartingWorkers(t *testing.T) {
	s, _, cfg := testsupport.Services(t)
	ctx := context.Background()
	v, err := s.SaveExecutionSettings(ctx, store.ExecutionInput{MaxVideoRunning: 2})
	if err != nil {
		t.Fatal(err)
	}
	w, err := New(s, nil, nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.client.Close()
	defer w.inspector.Close()
	entered := make(chan string, 20)
	release := make(chan struct{})
	var running atomic.Int32
	var peak atomic.Int32
	mux := asynq.NewServeMux()
	// Real claims and broker deliveries, with a blocking body in place of media
	// and model work. This verifies scheduling, not provider throughput.
	mux.HandleFunc("test.capacity", func(ctx context.Context, task *asynq.Task) error {
		var id string
		if err := json.Unmarshal(task.Payload(), &id); err != nil {
			return asynq.SkipRetry
		}
		c, err := s.Claim(ctx, id, 1, "capacity-worker")
		if errors.Is(err, store.ErrExecutionBusy) {
			return errBusy
		}
		if err != nil || c == nil {
			return err
		}
		n := running.Add(1)
		defer running.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		entered <- "video"
		select {
		case <-release:
			_, err = s.Fail(ctx, *c, domain.Failure{Code: "TEST_DONE", Message: "scheduler test"}, false, 0)
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	mux.HandleFunc("test.question", func(ctx context.Context, task *asynq.Task) error { entered <- "question"; return nil })
	for _, srv := range w.servers[:2] {
		if err := srv.Start(mux); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		close(release)
		for _, srv := range w.servers[:2] {
			srv.Shutdown()
		}
	}()
	owner, _ := s.NewSession(ctx, domain.ID(), time.Hour)
	for n := 0; n < 12; n++ {
		a, err := s.CreateRequest(ctx, owner.ID, domain.ID(), "local://"+domain.ID()+".mp4", "", true)
		if err != nil {
			t.Fatal(err)
		}
		r, _ := s.Request(ctx, owner.ID, a.ID)
		if _, err = s.Pool.Exec(ctx, "UPDATE job_runs SET kind='article.generate' WHERE id=$1", *r.ActiveRunID); err != nil {
			t.Fatal(err)
		}
		if _, err := w.client.Enqueue(asynq.NewTask("test.capacity", domain.JSON(*r.ActiveRunID)), asynq.Queue("generation")); err != nil {
			t.Fatal(err)
		}
	}
	expect := func(lane string) {
		t.Helper()
		select {
		case got := <-entered:
			if got != lane {
				t.Fatalf("expected %s, got %s", lane, got)
			}
		case <-time.After(15 * time.Second):
			t.Fatalf("%s never started", lane)
		}
	}
	expect("video")
	expect("video")
	if _, err := w.client.Enqueue(asynq.NewTask("test.question", nil), asynq.Queue("interactive")); err != nil {
		t.Fatal(err)
	}
	expect("question")
	v, err = s.SaveExecutionSettings(ctx, store.ExecutionInput{ExpectedRevision: v.Revision, MaxVideoRunning: 10})
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 8; n++ {
		expect("video")
	}
	q, err := s.QueueStatus(ctx)
	if err != nil || q.Processing != 10 || q.Waiting != 2 || peak.Load() != 10 {
		t.Fatalf("live raise: %+v peak=%d %v", q, peak.Load(), err)
	}
	if _, err = s.SaveExecutionSettings(ctx, store.ExecutionInput{ExpectedRevision: v.Revision, MaxVideoRunning: 2}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
		t.Fatal("new task started above the lowered limit")
	case <-time.After(2500 * time.Millisecond):
	}
	q, err = s.QueueStatus(ctx)
	if err != nil || q.Processing != 10 || q.MaxConcurrent != 2 {
		t.Fatalf("running jobs were interrupted: %+v %v", q, err)
	}
	var queued string
	if err := s.Pool.QueryRow(ctx, "SELECT id FROM job_runs WHERE status='queued' LIMIT 1").Scan(&queued); err != nil {
		t.Fatal(err)
	}
	// The real handler must map a capacity wait to a non-failure delivery.
	if err := w.handle(ctx, asynq.NewTask("vidlens.run", domain.JSON(payload{1, queued, 1}))); !errors.Is(err, errBusy) {
		t.Fatalf("capacity spent a failure: %v", err)
	}
	var attempts, failures int
	if err := s.Pool.QueryRow(ctx, "SELECT attempt_no,failure_count FROM job_runs WHERE id=$1", queued).Scan(&attempts, &failures); err != nil || attempts != 0 || failures != 0 {
		t.Fatal("broker capacity waits spent business budget")
	}
}
