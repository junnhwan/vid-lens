package repository

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/model"
)

func newGovernanceTestRepos(t *testing.T) *Repositories {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared&_pragma=busy_timeout(5000)"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	return NewRepositories(db)
}

func TestEnsureTaskJobRetryBudgetIsStableWithinCycleAndRotatesOnNewCycle(t *testing.T) {
	repos := newGovernanceTestRepos(t)
	now := time.Date(2026, 7, 14, 16, 0, 0, 0, time.UTC)
	task := &model.VideoTask{
		UserID: 7, FileMD5: "acacacacacacacacacacacacacacacac", Filename: "cycle.mp4",
		Status: model.TaskStatusPending, Stage: model.TaskStageUploaded, MaxRetries: 3,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.TaskJob.UpsertQueued(task, model.TaskJobTypeTranscribe, model.TaskStageTranscribing, 3); err != nil {
		t.Fatalf("create task job: %v", err)
	}

	first, err := repos.EnsureTaskJobRetryBudget(task.ID, model.TaskJobTypeTranscribe, now)
	if err != nil {
		t.Fatalf("ensure first retry budget: %v", err)
	}
	replay, err := repos.EnsureTaskJobRetryBudget(task.ID, model.TaskJobTypeTranscribe, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("ensure replay retry budget: %v", err)
	}
	if first == "" || replay != first {
		t.Fatalf("same-cycle budgets = %q/%q", first, replay)
	}
	job, err := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTranscribe)
	if err != nil || job == nil || job.RetryBudgetID != first {
		t.Fatalf("bound task job = %+v err=%v", job, err)
	}

	if err := repos.TaskJob.UpsertQueued(task, model.TaskJobTypeTranscribe, model.TaskStageTranscribing, 3); err != nil {
		t.Fatalf("start second task cycle: %v", err)
	}
	second, err := repos.EnsureTaskJobRetryBudget(task.ID, model.TaskJobTypeTranscribe, now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("ensure second retry budget: %v", err)
	}
	if second == first {
		t.Fatalf("new task cycle reused exhausted budget %q", first)
	}
	secondBudget, err := repos.RetryBudget.Get(second)
	if err != nil || secondBudget.AttemptCount != 0 || secondBudget.MaxAttempts != 3 {
		t.Fatalf("second budget = %+v err=%v", secondBudget, err)
	}
}

func TestRetryBudgetPersistsAndPreventsLayeredRetryAmplification(t *testing.T) {
	repos := newGovernanceTestRepos(t)
	now := time.Date(2026, 7, 13, 1, 2, 3, 0, time.UTC)
	budget, err := repos.RetryBudget.Ensure(RetryBudgetSpec{BudgetID: "budget-1", TaskID: 7, JobID: 9, Operation: "summary", MaxAttempts: 3, Deadline: now.Add(time.Hour), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if budget.AttemptCount != 0 {
		t.Fatalf("attempts=%d", budget.AttemptCount)
	}

	layers := []string{model.RetryAttemptLayerProvider, model.RetryAttemptLayerScheduler, model.RetryAttemptLayerProvider}
	for i, layer := range layers {
		d, err := repos.RetryBudget.Consume("budget-1", "attempt-"+string(rune('a'+i)), layer, now.Add(time.Duration(i)*time.Second))
		if err != nil || !d.Allowed {
			t.Fatalf("consume %d: %+v %v", i, d, err)
		}
	}
	exhausted, err := repos.RetryBudget.Consume("budget-1", "attempt-d", model.RetryAttemptLayerProvider, now.Add(4*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if exhausted.Allowed || exhausted.Reason != model.RetryBudgetReasonExhausted {
		t.Fatalf("decision=%+v", exhausted)
	}

	// A replay of an already consumed attempt is idempotent, even after rebuilding repositories.
	restarted := NewRepositories(repos.db)
	duplicate, err := restarted.RetryBudget.Consume("budget-1", "attempt-a", model.RetryAttemptLayerProvider, now.Add(5*time.Second))
	if err != nil || !duplicate.Allowed || !duplicate.Duplicate || duplicate.AttemptCount != 3 {
		t.Fatalf("duplicate=%+v err=%v", duplicate, err)
	}
}

func TestRetryBudgetDeadlineAndConcurrentLimit(t *testing.T) {
	repos := newGovernanceTestRepos(t)
	now := time.Now().UTC()
	if _, err := repos.RetryBudget.Ensure(RetryBudgetSpec{BudgetID: "budget-concurrent", Operation: "chat", MaxAttempts: 5, Deadline: now.Add(time.Hour), Now: now}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	allowed := make(chan bool, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			d, e := repos.RetryBudget.Consume("budget-concurrent", fmtAttempt(i), model.RetryAttemptLayerProvider, now)
			allowed <- e == nil && d.Allowed
		}(i)
	}
	wg.Wait()
	close(allowed)
	n := 0
	for ok := range allowed {
		if ok {
			n++
		}
	}
	if n != 5 {
		t.Fatalf("allowed=%d want=5", n)
	}

	if _, err := repos.RetryBudget.Ensure(RetryBudgetSpec{BudgetID: "expired", Operation: "asr", MaxAttempts: 2, Deadline: now.Add(-time.Second), Now: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	d, err := repos.RetryBudget.Consume("expired", "late", model.RetryAttemptLayerProvider, now)
	if err != nil || d.Allowed || d.Reason != model.RetryBudgetReasonDeadline {
		t.Fatalf("expired=%+v err=%v", d, err)
	}
}

func fmtAttempt(i int) string { return fmt.Sprintf("attempt-%02d", i) }
