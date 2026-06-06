package mq

import (
	"context"
	"fmt"
	"strings"
	"time"

	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

const (
	TaskJobAnalyze    = "analyze"
	TaskJobTranscribe = "transcribe"
	TaskJobDownload   = "download"
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

func isRetryableError(err error) bool {
	if err == nil {
		return false
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

func (c *Consumer) recordTaskFailure(taskID int64, jobType, stage string, err error) error {
	task, findErr := c.repo.Task.FindByID(taskID)
	if findErr != nil {
		return findErr
	}
	policy := c.retryPolicy.normalized()
	maxRetries := task.MaxRetries
	if maxRetries <= 0 {
		maxRetries = policy.MaxRetries
	}

	errMsg := truncateError(err)
	if !isRetryableError(err) {
		return c.repo.Task.RecordTerminalFailure(taskID, jobType, stage, "non_retryable_error", errMsg, task.RetryCount, maxRetries, model.TaskStatusFailed)
	}

	nextRetryCount := task.RetryCount + 1
	if nextRetryCount > maxRetries {
		return c.repo.Task.RecordTerminalFailure(taskID, jobType, stage, "retry_exhausted", errMsg, nextRetryCount, maxRetries, model.TaskStatusDead)
	}

	nextRetryAt := policy.Now().Add(policy.backoffForRetry(nextRetryCount))
	return c.repo.Task.RecordRetryableFailure(taskID, jobType, stage, errMsg, nextRetryCount, maxRetries, nextRetryAt)
}

type retryProducer interface {
	EnqueueAnalyze(ctx context.Context, taskID int64, md5 string) error
	EnqueueTranscribe(ctx context.Context, taskID int64, md5 string) error
	EnqueueDownload(ctx context.Context, taskID int64, key string) error
}

type RetrySchedulerConfig struct {
	BatchSize int
	Interval  time.Duration
	Now       func() time.Time
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
	if config.Now == nil {
		config.Now = time.Now
	}
	return &RetryScheduler{repos: repos, producer: producer, config: config}
}

func (s *RetryScheduler) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(s.config.Interval)
		defer ticker.Stop()
		for {
			if err := s.RunOnce(ctx); err != nil {
				fmt.Printf("[Kafka] retry scheduler failed: %v\n", err)
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
		status, stage := retryDispatchState(task.LastJobType, task.Stage)
		claimed, err := s.repos.Task.ClaimRetryTask(task.ID, now, status, stage)
		if err != nil {
			return err
		}
		if !claimed {
			continue
		}
		if err := s.enqueueRetry(ctx, task); err != nil {
			_ = s.repos.Task.UpdateStatusAndStage(task.ID, model.TaskStatusFailed, stage, truncateError(err))
			return err
		}
	}
	return nil
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
	default:
		return fmt.Errorf("未知重试任务类型: %s", task.LastJobType)
	}
}
