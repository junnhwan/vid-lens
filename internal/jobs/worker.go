package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"vidlens/internal/config"
	"vidlens/internal/domain"
	"vidlens/internal/exporting"
	"vidlens/internal/investigator"
	"vidlens/internal/media"
	"vidlens/internal/store"
)

type payload struct {
	SchemaVersion int    `json:"schema_version"`
	RunID         string `json:"run_id"`
	Generation    int64  `json:"dispatch_generation"`
}
type retryError struct{ delay time.Duration }

func (e *retryError) Error() string { return "execution will retry" }

var errBusy = errors.New("business ownership remains active")

type Worker struct {
	Store     *store.Store
	Media     *media.Service
	Agent     *investigator.Engine
	Config    config.Config
	client    *asynq.Client
	inspector *asynq.Inspector
	servers   []*asynq.Server
	owner     string
	ctx       context.Context
	wg        sync.WaitGroup
}

func New(s *store.Store, m *media.Service, a *investigator.Engine, c config.Config) (*Worker, error) {
	opt, e := asynq.ParseRedisURI(c.RedisURL)
	if e != nil {
		return nil, e
	}
	w := &Worker{Store: s, Media: m, Agent: a, Config: c, client: asynq.NewClient(opt), inspector: asynq.NewInspector(opt), owner: domain.ID()}
	for _, queue := range []string{"generation", "interactive", "maintenance"} {
		queues := map[string]int{queue: 1}
		if queue == "maintenance" {
			queues["cleanup"] = 2
		}
		srv := asynq.NewServer(opt, asynq.Config{Concurrency: 1, Queues: queues, StrictPriority: queue == "maintenance", ShutdownTimeout: 30 * time.Second, RetryDelayFunc: func(n int, e error, t *asynq.Task) time.Duration {
			var re *retryError
			if errors.As(e, &re) {
				return re.delay
			}
			if errors.Is(e, errBusy) {
				return 10 * time.Second
			}
			if t.Type() == "asset.cleanup" {
				return []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute}[min(n, 2)]
			}
			return 15 * time.Second
		}, IsFailure: func(e error) bool { return !errors.Is(e, errBusy) }, ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, t *asynq.Task, e error) {
			slog.Warn("queue delivery did not finish", "task_type", t.Type(), "class", "retry_or_recovery")
		})})
		w.servers = append(w.servers, srv)
	}
	return w, nil
}
func (w *Worker) Start(ctx context.Context) error {
	w.ctx = ctx
	mux := asynq.NewServeMux()
	mux.HandleFunc("vidlens.run", w.handle)
	mux.HandleFunc("model.probe", func(ctx context.Context, t *asynq.Task) error {
		var body struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(t.Payload(), &body) != nil {
			return asynq.SkipRetry
		}
		p, e := w.Store.ClaimProbe(ctx, body.ID)
		if e != nil {
			return e
		}
		if p == nil {
			var pending bool
			if e = w.Store.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM model_probes WHERE id=$1 AND status IN ('queued','running') AND deadline_at>now())", body.ID).Scan(&pending); e != nil {
				return e
			}
			if !pending {
				return nil
			}
			return errBusy
		}
		return w.Agent.RunProbe(ctx, *p)
	})
	mux.HandleFunc("article.export", func(ctx context.Context, t *asynq.Task) error {
		var body struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(t.Payload(), &body) != nil {
			return asynq.SkipRetry
		}
		v, e := w.Store.ClaimExport(ctx, body.ID)
		if e != nil {
			return e
		}
		if v == nil {
			var pending bool
			if e = w.Store.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM article_exports WHERE id=$1 AND status IN ('queued','running','retry_wait') AND deadline_at>now())", body.ID).Scan(&pending); e != nil {
				return e
			}
			if !pending {
				return nil
			}
			return errBusy
		}
		return (&exporting.Service{Store: w.Store, Config: w.Config}).Run(ctx, *v)
	})
	mux.HandleFunc("asset.cleanup", func(ctx context.Context, t *asynq.Task) error {
		if e := w.Media.Cleanup(ctx); e != nil {
			return e
		}
		return (&exporting.Service{Store: w.Store, Config: w.Config}).Cleanup(ctx)
	})
	for _, srv := range w.servers {
		if e := srv.Start(mux); e != nil {
			return e
		}
	}
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		dispatch := time.NewTicker(time.Second)
		reconcile := time.NewTicker(30 * time.Second)
		cleanup := time.NewTicker(5 * time.Minute)
		defer dispatch.Stop()
		defer reconcile.Stop()
		defer cleanup.Stop()
		w.dispatch(ctx)
		w.reconcile(ctx)
		w.enqueueCleanup(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-dispatch.C:
				w.dispatch(ctx)
			case <-reconcile.C:
				w.reconcile(ctx)
			case <-cleanup.C:
				w.enqueueCleanup(ctx)
			}
		}
	}()
	return nil
}
func (w *Worker) Shutdown() {
	for _, srv := range w.servers {
		srv.Stop()
	}
	for _, srv := range w.servers {
		srv.Shutdown()
	}
	w.wg.Wait()
	w.client.Close()
	w.inspector.Close()
}
func taskID(id string, generation int64) string { return fmt.Sprintf("vl:%s:%d", id, generation) }
func (w *Worker) dispatch(ctx context.Context) {
	if exports, e := w.Store.PendingExports(ctx); e == nil {
		for _, v := range exports {
			_, _ = w.client.EnqueueContext(ctx, asynq.NewTask("article.export", domain.JSON(map[string]string{"id": v.ID})), asynq.TaskID(fmt.Sprintf("export:%s:%d", v.ID, v.Attempts)), asynq.Queue("maintenance"), asynq.MaxRetry(3), asynq.Timeout(2*time.Minute+5*time.Second), asynq.Deadline(v.DeadlineAt), asynq.Retention(24*time.Hour))
		}
	}
	if probes, e := w.Store.PendingProbes(ctx); e == nil {
		for _, p := range probes {
			_, _ = w.client.EnqueueContext(ctx, asynq.NewTask("model.probe", domain.JSON(map[string]string{"id": p.ID})), asynq.TaskID("probe:"+p.ID), asynq.Queue("maintenance"), asynq.MaxRetry(18), asynq.Timeout(180*time.Second), asynq.Deadline(p.DeadlineAt), asynq.Retention(24*time.Hour))
		}
	}
	out, e := w.Store.Outbox(ctx)
	if e != nil {
		return
	}
	for _, o := range out {
		r, e := w.Store.Run(ctx, o.RunID)
		if e != nil {
			continue
		}
		if domain.Terminal(r.Status) || r.DispatchGeneration != o.DispatchGeneration {
			_ = w.Store.OutboxResult(ctx, o, true)
			continue
		}
		p, e := w.Store.Profile(ctx, r.ConfigID)
		if e != nil {
			continue
		}
		body := domain.JSON(payload{1, r.ID, r.DispatchGeneration})
		task := asynq.NewTask("vidlens.run", body)
		_, e = w.client.EnqueueContext(ctx, task, asynq.TaskID(taskID(r.ID, r.DispatchGeneration)), asynq.Queue(o.Queue), asynq.MaxRetry(p.MaxRetries(r.Kind)), asynq.Timeout(p.ActiveBudget(r.Kind)+15*time.Second), asynq.Deadline(r.DeadlineAt), asynq.Retention(24*time.Hour))
		delivered := e == nil
		if errors.Is(e, asynq.ErrTaskIDConflict) {
			existing, err := w.inspector.GetTaskInfo(o.Queue, taskID(r.ID, r.DispatchGeneration))
			delivered = err == nil && existing.Type == "vidlens.run" && string(existing.Payload) == string(body)
		}
		_ = w.Store.OutboxResult(ctx, o, delivered)
	}
}
func (w *Worker) handle(ctx context.Context, t *asynq.Task) error {
	var p payload
	if json.Unmarshal(t.Payload(), &p) != nil || p.SchemaVersion != 1 || p.RunID == "" {
		return asynq.SkipRetry
	}
	c, e := w.Store.Claim(ctx, p.RunID, p.Generation, w.owner)
	if e != nil {
		return e
	}
	if c == nil {
		r, e := w.Store.Run(ctx, p.RunID)
		if e == nil && r.Status == "running" && r.DispatchGeneration == p.Generation {
			return errBusy
		}
		return e
	}
	profile, e := w.Store.Profile(ctx, c.Run.ConfigID)
	if e != nil {
		return e
	}
	_, current, e := w.Store.CurrentConfig(ctx)
	if e != nil {
		return e
	}
	profile.MaxDownloadBytes = min(profile.MaxDownloadBytes, current.MaxDownloadBytes)
	profile.MaxVideoMS = min(profile.MaxVideoMS, current.MaxVideoMS)
	profile.WorkspaceBytes = min(profile.WorkspaceBytes, current.WorkspaceBytes)
	budget := profile.ActiveBudget(c.Run.Kind) - time.Duration(c.Run.ActiveMS)*time.Millisecond
	deadline := minTime(time.Now().Add(budget), c.Run.DeadlineAt)
	child, cancel := context.WithDeadline(ctx, deadline)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-child.Done():
				return
			case <-w.ctx.Done():
				cancel()
				return
			case <-ticker.C:
				if e := w.Store.Heartbeat(child, *c); e != nil {
					cancel()
					return
				}
			}
		}
	}()
	if c.Run.Kind == "source.prepare" || c.Run.Kind == "source.restore" {
		e = w.Media.Prepare(child, *c, profile)
	} else {
		e = w.Agent.Run(child, *c, profile)
	}
	cancel()
	<-done
	if e == nil {
		return nil
	}
	saveCtx, cc := context.WithTimeout(context.Background(), 5*time.Second)
	defer cc()
	if w.ctx.Err() != nil {
		if err := w.Store.Suspend(saveCtx, *c); err != nil && !errors.Is(err, domain.ErrFenced) {
			return err
		}
		return errBusy
	}
	if errors.Is(e, media.ErrMediaMissing) && c.Run.Kind == "qa.answer" {
		if err := w.Store.WaitForRestore(saveCtx, *c); err == nil {
			return nil
		} else {
			e = err
		}
	}
	if errors.Is(e, domain.ErrFenced) {
		return nil
	}
	f, temporary, after := classify(e)
	delay := 15 * time.Second
	if c.Run.Kind == "qa.answer" || c.Run.Kind == "source.restore" {
		delay = 5 * time.Second
	} else if c.Run.FailureCount > 0 {
		delay = 60 * time.Second
	}
	delay = time.Duration(float64(delay) * (0.8 + 0.4*rand.Float64()))
	delay = max(delay, after)
	maxWait := 5 * time.Minute
	if c.Run.Kind == "qa.answer" {
		maxWait = time.Minute
	}
	if after > maxWait {
		temporary = false
	}
	retry, err := w.Store.Fail(saveCtx, *c, f, temporary, delay)
	if errors.Is(err, domain.ErrFenced) {
		return nil
	}
	if err != nil {
		return err
	}
	if retry {
		return &retryError{delay: delay}
	}
	return nil
}
func classify(e error) (domain.Failure, bool, time.Duration) {
	f := domain.Failure{Code: "INTERNAL_FAILURE", Message: "处理未能完成。", CanRetry: true}
	var work *domain.WorkError
	if errors.As(e, &work) {
		f.Code = work.Code
		f.Message = work.Message
		f.CanRetry = work.Transient || work.Code == "MODEL_OUTPUT_TRUNCATED" || work.Code == "MODEL_RESULT_INVALID"
		return f, work.Transient, work.RetryAfter
	}
	var p *domain.Problem
	if errors.As(e, &p) {
		f.Code = p.Code
		f.Message = p.Detail
		f.CanRetry = p.Status == 503 || p.Code == "MODEL_RESULT_INVALID" || p.Code == "MODEL_RESULT_NOT_SUBMITTED" || p.Code == "MODEL_OUTPUT_TRUNCATED"
		return f, p.Status == 503, 0
	}
	if errors.Is(e, context.DeadlineExceeded) {
		f.Code = "TASK_DEADLINE_EXCEEDED"
		f.Message = "处理达到时限。"
		return f, false, 0
	}
	if errors.Is(e, context.Canceled) {
		f.Code = "INTERNAL_FAILURE"
		f.Message = "执行中断，可稍后重试。"
		return f, true, 0
	}
	return f, false, 0
}
func (w *Worker) reconcile(ctx context.Context) {
	runs, e := w.Store.Recoverable(ctx)
	if e != nil {
		return
	}
	for _, r := range runs {
		queue := "generation"
		if r.Kind == "qa.answer" {
			queue = "interactive"
		}
		info, e := w.inspector.GetTaskInfo(queue, taskID(r.ID, r.DispatchGeneration))
		state := "unknown"
		var retryAt *time.Time
		if errors.Is(e, asynq.ErrTaskNotFound) || errors.Is(e, asynq.ErrQueueNotFound) {
			state = "missing"
		} else if e == nil {
			state = info.State.String()
			if state == "retry" || state == "scheduled" {
				retryAt = &info.NextProcessAt
			}
		}
		if e = w.Store.Repair(ctx, r.ID, r.DispatchGeneration, state, retryAt); e != nil {
			slog.Warn("run reconciliation deferred", "run_id", r.ID)
		}
	}
}
func (w *Worker) enqueueCleanup(ctx context.Context) {
	id := fmt.Sprintf("cleanup:%d", time.Now().Unix()/300)
	_, _ = w.client.EnqueueContext(ctx, asynq.NewTask("asset.cleanup", domain.JSON(map[string]int{"schema_version": 1})), asynq.TaskID(id), asynq.Queue("cleanup"), asynq.MaxRetry(3), asynq.Timeout(2*time.Minute), asynq.Retention(24*time.Hour))
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
