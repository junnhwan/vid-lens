package repository

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"vid-lens/internal/model"
)

type RetryBudgetRepository struct{ db *gorm.DB }

func NewRetryBudgetRepository(db *gorm.DB) *RetryBudgetRepository {
	return &RetryBudgetRepository{db: db}
}

type RetryBudgetSpec struct {
	BudgetID    string
	TaskID      int64
	JobID       int64
	Operation   string
	MaxAttempts int
	Deadline    time.Time
	Now         time.Time
}

func (r *RetryBudgetRepository) Ensure(spec RetryBudgetSpec) (*model.AIRetryBudget, error) {
	spec.BudgetID = strings.TrimSpace(spec.BudgetID)
	spec.Operation = strings.TrimSpace(spec.Operation)
	if spec.BudgetID == "" || spec.Operation == "" || spec.MaxAttempts <= 0 || spec.Deadline.IsZero() {
		return nil, fmt.Errorf("invalid retry budget specification")
	}
	if spec.Now.IsZero() {
		spec.Now = time.Now()
	}
	row := model.AIRetryBudget{BudgetID: spec.BudgetID, TaskID: spec.TaskID, JobID: spec.JobID, Operation: spec.Operation, MaxAttempts: spec.MaxAttempts, Deadline: spec.Deadline, CreatedAt: spec.Now, UpdatedAt: spec.Now}
	if err := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return nil, err
	}
	var stored model.AIRetryBudget
	if err := r.db.First(&stored, "budget_id = ?", spec.BudgetID).Error; err != nil {
		return nil, err
	}
	if stored.Operation != spec.Operation || stored.MaxAttempts != spec.MaxAttempts || !stored.Deadline.Equal(spec.Deadline) || (stored.TaskID != 0 && spec.TaskID != 0 && stored.TaskID != spec.TaskID) || (stored.JobID != 0 && spec.JobID != 0 && stored.JobID != spec.JobID) {
		return nil, fmt.Errorf("retry budget %s already exists with a different immutable specification", spec.BudgetID)
	}
	return &stored, nil
}

const taskJobRetryBudgetTTL = 24 * time.Hour

// EnsureTaskJobRetryBudget creates one durable budget for the current explicit
// task-job cycle. The budget counts only additional provider/scheduler retry
// work; normal ASR chunks and first provider calls do not consume it.
func (r *Repositories) EnsureTaskJobRetryBudget(taskID int64, jobType string, now time.Time) (string, error) {
	if r == nil || r.db == nil || taskID <= 0 || strings.TrimSpace(jobType) == "" {
		return "", fmt.Errorf("invalid task job retry budget request")
	}
	if now.IsZero() {
		now = time.Now()
	}
	var budgetID string
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var job model.TaskJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("task_id = ? AND job_type = ?", taskID, jobType).First(&job).Error; err != nil {
			return err
		}
		if existing := strings.TrimSpace(job.RetryBudgetID); existing != "" {
			budgetID = existing
			return nil
		}
		maxAttempts := job.MaxRetries
		if maxAttempts <= 0 {
			maxAttempts = 3
		}
		budgetID = fmt.Sprintf("task-%d-job-%d-cycle-%d", taskID, job.ID, job.RetryBudgetGeneration)
		budgetRepo := NewRetryBudgetRepository(tx)
		if _, err := budgetRepo.Ensure(RetryBudgetSpec{
			BudgetID: budgetID, TaskID: taskID, JobID: job.ID,
			Operation: jobType, MaxAttempts: maxAttempts,
			Deadline: now.Add(taskJobRetryBudgetTTL), Now: now,
		}); err != nil {
			return err
		}
		result := tx.Model(&model.TaskJob{}).
			Where("id = ? AND (retry_budget_id = '' OR retry_budget_id IS NULL)", job.ID).
			Update("retry_budget_id", budgetID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("task job retry budget changed concurrently")
		}
		return nil
	})
	return budgetID, err
}

func (r *RetryBudgetRepository) Get(budgetID string) (*model.AIRetryBudget, error) {
	var budget model.AIRetryBudget
	if err := r.db.First(&budget, "budget_id = ?", strings.TrimSpace(budgetID)).Error; err != nil {
		return nil, err
	}
	return &budget, nil
}

func (r *RetryBudgetRepository) Consume(budgetID, attemptKey, layer string, now time.Time) (model.RetryBudgetDecision, error) {
	var decision model.RetryBudgetDecision
	var err error
	for retries := 0; retries < 50; retries++ {
		decision, err = r.consumeOnce(budgetID, attemptKey, layer, now)
		if err == nil || (!strings.Contains(strings.ToLower(err.Error()), "locked") && !strings.Contains(strings.ToLower(err.Error()), "deadlock")) {
			return decision, err
		}
		time.Sleep(time.Duration(retries+1) * time.Millisecond)
	}
	return decision, err
}

func (r *RetryBudgetRepository) consumeOnce(budgetID, attemptKey, layer string, now time.Time) (model.RetryBudgetDecision, error) {
	budgetID, attemptKey, layer = strings.TrimSpace(budgetID), strings.TrimSpace(attemptKey), strings.TrimSpace(layer)
	if budgetID == "" || attemptKey == "" || (layer != model.RetryAttemptLayerProvider && layer != model.RetryAttemptLayerScheduler) {
		return model.RetryBudgetDecision{}, fmt.Errorf("invalid retry attempt")
	}
	if now.IsZero() {
		now = time.Now()
	}
	var decision model.RetryBudgetDecision
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var prior model.AIRetryAttempt
		err := tx.Where("budget_id = ? AND attempt_key = ?", budgetID, attemptKey).First(&prior).Error
		if err == nil {
			var b model.AIRetryBudget
			if err := tx.First(&b, "budget_id = ?", budgetID).Error; err != nil {
				return err
			}
			decision = model.RetryBudgetDecision{Allowed: true, Duplicate: true, AttemptCount: b.AttemptCount, MaxAttempts: b.MaxAttempts, Deadline: b.Deadline}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		first := now
		res := tx.Model(&model.AIRetryBudget{}).
			Where("budget_id = ? AND attempt_count < max_attempts AND deadline > ?", budgetID, now).
			Updates(map[string]any{"attempt_count": gorm.Expr("attempt_count + 1"), "first_attempt_at": gorm.Expr("COALESCE(first_attempt_at, ?)", first), "updated_at": now})
		if res.Error != nil {
			return res.Error
		}
		var b model.AIRetryBudget
		if err := tx.First(&b, "budget_id = ?", budgetID).Error; err != nil {
			return err
		}
		decision = model.RetryBudgetDecision{AttemptCount: b.AttemptCount, MaxAttempts: b.MaxAttempts, Deadline: b.Deadline}
		if res.RowsAffected == 0 {
			if !b.Deadline.After(now) {
				decision.Reason = model.RetryBudgetReasonDeadline
			} else {
				decision.Reason = model.RetryBudgetReasonExhausted
			}
			return nil
		}
		attempt := model.AIRetryAttempt{BudgetID: budgetID, AttemptKey: attemptKey, Layer: layer, CreatedAt: now}
		if err := tx.Create(&attempt).Error; err != nil {
			return err
		}
		decision.Allowed = true
		return nil
	})
	if err != nil {
		// A concurrent replay can lose the unique-key race. The transaction rolled
		// back its increment, so return the already persisted attempt as duplicate.
		var prior model.AIRetryAttempt
		if findErr := r.db.Where("budget_id = ? AND attempt_key = ?", budgetID, attemptKey).First(&prior).Error; findErr == nil {
			b, getErr := r.Get(budgetID)
			if getErr != nil {
				return model.RetryBudgetDecision{}, getErr
			}
			return model.RetryBudgetDecision{Allowed: true, Duplicate: true, AttemptCount: b.AttemptCount, MaxAttempts: b.MaxAttempts, Deadline: b.Deadline}, nil
		}
	}
	return decision, err
}
