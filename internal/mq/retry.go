package mq

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/observability"
	"vid-lens/internal/repository"
)

const (
	TaskJobAnalyze    = model.TaskJobTypeAnalyze
	TaskJobTranscribe = model.TaskJobTypeTranscribe
	TaskJobDownload   = model.TaskJobTypeDownload
	TaskJobRAGIndex   = model.TaskJobTypeRAGIndex
)

type TaskRetryPolicy struct {
	MaxRetries     int
	BackoffSeconds []int
	Now            func() time.Time
}

func (p TaskRetryPolicy) normalized() TaskRetryPolicy {
	if p.MaxRetries <= 0 {
		p.MaxRetries = 3
	}
	if len(p.BackoffSeconds) == 0 {
		p.BackoffSeconds = []int{60, 300, 900}
	}
	if p.Now == nil {
		p.Now = time.Now
	}
	return p
}

func (p TaskRetryPolicy) backoffForRetry(retryCount int) time.Duration {
	p = p.normalized()
	if retryCount <= 0 {
		retryCount = 1
	}
	idx := retryCount - 1
	if idx >= len(p.BackoffSeconds) {
		idx = len(p.BackoffSeconds) - 1
	}
	return time.Duration(p.BackoffSeconds[idx]) * time.Second
}

func (p TaskRetryPolicy) retryDelay(retryCount int, err error) time.Duration {
	delay := p.backoffForRetry(retryCount)
	var providerErr *ai.ProviderError
	if errors.As(err, &providerErr) && providerErr.RetryAfter > delay {
		return providerErr.RetryAfter
	}
	return delay
}

func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	var providerErr *ai.ProviderError
	if errors.As(err, &providerErr) {
		return providerErr.Retryable
	}
	text := strings.ToLower(err.Error())
	nonRetryable := []string{
		"请先配置 ai 服务",
		"api key 解密失败",
		"无权",
		"文件不存在",
		"embedding 维度",
		"asr 返回空结果",
		"缺少转录文本",
		"video unavailable",
		"http error 412",
		"precondition failed",
	}
	for _, marker := range nonRetryable {
		if strings.Contains(text, strings.ToLower(marker)) {
			return false
		}
	}

	retryable := []string{
		"timeout",
		"context deadline exceeded",
		"network",
		"connection refused",
		"connection reset",
		"temporary",
		"service unavailable",
		"too many requests",
		"http 429",
		"http 500",
		"http 502",
		"http 503",
		"http 504",
		"minio",
		"milvus",
	}
	for _, marker := range retryable {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func (c *Consumer) SetRetryPolicy(policy TaskRetryPolicy) {
	c.retryPolicy = policy.normalized()
}

func (c *Consumer) recordTaskFailure(taskID int64, jobType, stage string, failure error, processingToken ...string) error {
	if c == nil || c.repo == nil {
		return fmt.Errorf("任务仓储未初始化")
	}
	if failure == nil {
		return fmt.Errorf("任务失败原因不能为空")
	}
	if len(processingToken) > 0 && strings.TrimSpace(processingToken[0]) != "" {
		return c.recordLeasedTaskFailure(taskID, jobType, stage, failure, processingToken[0])
	}

	var metricStage, metricCode, metricJobType string
	var metricDead, metricRetry bool
	metricStartedAt := time.Now()
	var logCtx context.Context = context.Background()
	err := c.repo.Transaction(func(repos *repository.Repositories) error {
		task, findErr := repos.Task.FindByID(taskID)
		if findErr != nil {
			return findErr
		}
		if strings.TrimSpace(stage) == "" {
			stage = task.Stage
		}
		metricStage, metricJobType = stage, jobType
		if task.StageStartedAt != nil {
			metricStartedAt = *task.StageStartedAt
		}
		job, _ := repos.TaskJob.FindByTaskAndType(taskID, jobType)
		logCtx = contextForTaskJob(context.Background(), task, job)
		policy := c.retryPolicy.normalized()
		maxRetries := task.MaxRetries
		if maxRetries <= 0 {
			maxRetries = policy.MaxRetries
		}

		errMsg := truncateError(failure)
		if !isRetryableError(failure) {
			metricCode = "non_retryable_error"
			if err := repos.Task.RecordTerminalFailure(taskID, jobType, stage, "non_retryable_error", errMsg, task.RetryCount, maxRetries, model.TaskStatusFailed); err != nil {
				return err
			}
			return repos.TaskJob.RecordTerminalFailure(taskID, jobType, stage, "non_retryable_error", errMsg, task.RetryCount, maxRetries, model.TaskStatusFailed)
		}

		nextRetryCount := task.RetryCount + 1
		if nextRetryCount > maxRetries {
			metricCode, metricDead = "retry_exhausted", true
			if err := repos.Task.RecordTerminalFailure(taskID, jobType, stage, "retry_exhausted", errMsg, nextRetryCount, maxRetries, model.TaskStatusDead); err != nil {
				return err
			}
			return repos.TaskJob.RecordTerminalFailure(taskID, jobType, stage, "retry_exhausted", errMsg, nextRetryCount, maxRetries, model.TaskStatusDead)
		}

		metricCode, metricRetry = "retryable_error", true
		nextRetryAt := policy.Now().Add(policy.retryDelay(nextRetryCount, failure))
		if err := repos.Task.RecordRetryableFailure(taskID, jobType, stage, errMsg, nextRetryCount, maxRetries, nextRetryAt); err != nil {
			return err
		}
		return repos.TaskJob.RecordRetryableFailure(taskID, jobType, stage, errMsg, nextRetryCount, maxRetries, nextRetryAt)
	})
	if err != nil {
		return err
	}
	if metrics := observability.DefaultMetrics(); metrics != nil {
		metrics.ObserveTaskStage(metricStage, "failed", time.Since(metricStartedAt))
		if metricRetry {
			metrics.IncTaskRetry(metricJobType, metricCode)
		}
		if metricDead {
			metrics.IncTaskDead(metricJobType)
		}
	}
	observability.Log(logCtx, slog.Default(), slog.LevelWarn, "task stage failed",
		slog.String("error_code", metricCode), slog.String("error", observability.SafeError(failure)))
	return nil
}

func (c *Consumer) recordLeasedTaskFailure(taskID int64, jobType, stage string, failure error, token string) error {
	task, err := c.repo.Task.FindByID(taskID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(stage) == "" {
		stage = task.Stage
	}
	metricStartedAt := c.currentTime()
	if task.StageStartedAt != nil {
		metricStartedAt = *task.StageStartedAt
	}
	job, _ := c.repo.TaskJob.FindByTaskAndType(taskID, jobType)
	logCtx := contextForTaskJobStage(context.Background(), task, job, stage)
	policy := c.retryPolicy.normalized()
	maxRetries := policy.MaxRetries
	currentRetryCount := 0
	if job != nil {
		currentRetryCount = job.RetryCount
		if job.MaxRetries > 0 {
			maxRetries = job.MaxRetries
		}
	} else if task.MaxRetries > 0 {
		maxRetries = task.MaxRetries
	}
	req := repository.TaskProcessingFailureRequest{
		TaskID: taskID, JobType: jobType, Stage: stage, Token: token,
		Status: model.TaskStatusFailed, ErrorCode: "non_retryable_error", ErrorMessage: truncateError(failure),
		RetryCount: currentRetryCount, MaxRetries: maxRetries, Now: policy.Now(),
	}
	metricRetry, metricDead := false, false
	if isRetryableError(failure) {
		req.RetryCount = currentRetryCount + 1
		if req.RetryCount > maxRetries {
			req.Status = model.TaskStatusDead
			req.ErrorCode = "retry_exhausted"
			metricDead = true
		} else {
			req.ErrorCode = "retryable_error"
			next := req.Now.Add(policy.retryDelay(req.RetryCount, failure))
			req.NextRetryAt = &next
			metricRetry = true
		}
	}
	updated, err := c.repo.FailTaskProcessing(req)
	if err != nil {
		return err
	}
	if !updated {
		return nil
	}
	if metrics := observability.DefaultMetrics(); metrics != nil {
		metrics.ObserveTaskStage(stage, "failed", req.Now.Sub(metricStartedAt))
		if metricRetry {
			metrics.IncTaskRetry(jobType, req.ErrorCode)
		}
		if metricDead {
			metrics.IncTaskDead(jobType)
		}
	}
	observability.Log(logCtx, slog.Default(), slog.LevelWarn, "task stage failed",
		slog.String("error_code", req.ErrorCode), slog.String("error", observability.SafeError(failure)))
	return nil
}

type retryProducer interface {
	EnqueueAnalyze(ctx context.Context, taskID int64, md5 string) error
	EnqueueTranscribe(ctx context.Context, taskID int64, md5 string) error
	EnqueueDownload(ctx context.Context, taskID int64, key string) error
	EnqueueRAGIndex(ctx context.Context, taskID int64) error
}

type RetrySchedulerConfig struct {
	BatchSize              int
	Interval               time.Duration
	DispatchFailureBackoff time.Duration
	DispatchLease          time.Duration
	Now                    func() time.Time
	NewToken               func() string
}

type RetryScheduler struct {
	repos    *repository.Repositories
	producer retryProducer
	config   RetrySchedulerConfig
}

func NewRetryScheduler(repos *repository.Repositories, producer retryProducer, config RetrySchedulerConfig) *RetryScheduler {
	if config.BatchSize <= 0 {
		config.BatchSize = 20
	}
	if config.Interval <= 0 {
		config.Interval = 30 * time.Second
	}
	if config.DispatchFailureBackoff <= 0 {
		config.DispatchFailureBackoff = time.Minute
	}
	if config.DispatchLease <= 0 {
		config.DispatchLease = 2 * time.Minute
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.NewToken == nil {
		config.NewToken = uuid.NewString
	}
	return &RetryScheduler{repos: repos, producer: producer, config: config}
}

func (s *RetryScheduler) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(s.config.Interval)
		defer ticker.Stop()
		for {
			if err := s.RunOnce(ctx); err != nil {
				observability.Log(ctx, slog.Default(), slog.LevelError, "retry scheduler failed", slog.String("error", observability.SafeError(err)))
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *RetryScheduler) RunOnce(ctx context.Context) error {
	now := s.config.Now()
	tasks, err := s.repos.Task.FindDueRetryTasks(now, s.config.BatchSize)
	if err != nil {
		return err
	}

	for _, task := range tasks {
		_, stage := retryDispatchState(task.LastJobType, task.Stage)
		claimToken := s.config.NewToken()
		claimed, err := s.repos.ClaimRetryDispatch(repository.TaskDispatchClaimRequest{
			TaskID: task.ID, JobType: task.LastJobType, Stage: stage,
			ExpectedVersion: task.LeaseVersion, Now: now,
			LeaseUntil: now.Add(s.config.DispatchLease), Token: claimToken,
		})
		if err != nil {
			return err
		}
		if !claimed {
			continue
		}
		job, err := s.repos.TaskJob.FindByTaskAndType(task.ID, task.LastJobType)
		if err != nil {
			return err
		}
		if job != nil && strings.TrimSpace(job.RetryBudgetID) != "" {
			retryOrdinal := job.RetryCount
			if retryOrdinal <= 0 {
				retryOrdinal = task.RetryCount
			}
			attemptKey := fmt.Sprintf("scheduler:%d:retry:%d", job.ID, retryOrdinal)
			decision, consumeErr := s.repos.RetryBudget.Consume(job.RetryBudgetID, attemptKey, model.RetryAttemptLayerScheduler, now)
			if consumeErr != nil {
				return fmt.Errorf("consume retry budget: %w", consumeErr)
			}
			if !decision.Allowed {
				return &ai.RetryBudgetError{Decision: decision}
			}
		}
		retryCtx := contextWithClaimToken(contextForTaskJob(ctx, &task, job), claimToken)
		if job != nil {
			retryCtx = contextWithRetryBudgetID(retryCtx, job.RetryBudgetID)
		}
		if enqueueErr := s.enqueueRetry(retryCtx, task); enqueueErr != nil {
			nextRetryAt := now.Add(s.config.DispatchFailureBackoff)
			restored, restoreErr := s.repos.RestoreRetryDispatch(repository.TaskDispatchRestoreRequest{
				TaskID: task.ID, JobType: task.LastJobType, Stage: stage, Token: claimToken,
				ErrorMessage: truncateError(enqueueErr), NextRetryAt: nextRetryAt,
			})
			var notRestoredErr error
			if restoreErr == nil && !restored {
				notRestoredErr = fmt.Errorf("dispatch lease 已变化，未恢复旧调度状态")
			}
			return errors.Join(
				fmt.Errorf("重试任务投递失败: %w", enqueueErr),
				wrapRetryRestoreError("恢复重试调度状态失败", restoreErr),
				notRestoredErr,
			)
		}
	}
	return nil
}

func wrapRetryRestoreError(message string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", message, err)
}

func retryDispatchState(jobType, currentStage string) (int8, string) {
	switch jobType {
	case TaskJobDownload:
		return model.TaskStatusRunning, model.TaskStageDownloading
	case TaskJobTranscribe:
		return model.TaskStatusQueued, model.TaskStageTranscribing
	case TaskJobAnalyze:
		if currentStage == "" || currentStage == model.TaskStageUploaded || currentStage == model.TaskStageNone {
			currentStage = model.TaskStageSummarizing
		}
		return model.TaskStatusQueued, currentStage
	case TaskJobRAGIndex:
		return model.TaskStatusQueued, model.TaskStageIndexing
	default:
		return model.TaskStatusQueued, currentStage
	}
}

func (s *RetryScheduler) enqueueRetry(ctx context.Context, task model.VideoTask) error {
	switch task.LastJobType {
	case TaskJobDownload:
		return s.producer.EnqueueDownload(ctx, task.ID, task.FileMD5)
	case TaskJobTranscribe:
		return s.producer.EnqueueTranscribe(ctx, task.ID, task.FileMD5)
	case TaskJobAnalyze:
		return s.producer.EnqueueAnalyze(ctx, task.ID, task.FileMD5)
	case TaskJobRAGIndex:
		return s.producer.EnqueueRAGIndex(ctx, task.ID)
	default:
		return fmt.Errorf("未知重试任务类型: %s", task.LastJobType)
	}
}
