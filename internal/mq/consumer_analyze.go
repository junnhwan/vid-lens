package mq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/observability"
	"vid-lens/internal/pkg/lock"
	"vid-lens/internal/repository"

	"github.com/segmentio/kafka-go"
)

// handleAnalyze 处理视频分析任务
// 严格遵循六步流程（对应面试文档 MQ 消费者开发规范）
func (c *Consumer) handleAnalyze(ctx context.Context, msg kafka.Message) error {
	// 第 1 步：解析消息
	var payload AnalyzePayload
	if err := json.Unmarshal(msg.Value, &payload); err != nil {
		return fmt.Errorf("解析消息失败: %w", err)
	}

	observability.Log(ContextWithTraceID(ctx, payload.TraceID), slog.Default(), slog.LevelInfo, "analyze message received", slog.Int64("task_id", payload.TaskID))

	// 第 2 步：基于 MD5 获取分布式锁
	lockKey := fmt.Sprintf("vidlens:lock:%s", payload.MD5)
	distLock := lock.NewRedisLock(c.rdb, lockKey)

	acquired, err := distLock.TryLock(ctx, 5*time.Second)
	if err != nil {
		return fmt.Errorf("获取分布式锁失败: %w", err)
	}
	if !acquired {
		observability.Log(ctx, slog.Default(), slog.LevelWarn, "video processing lock busy")
		return fmt.Errorf("同一视频正在处理中")
	}
	defer distLock.Unlock(ctx)

	// 第 3 步：幂等校验
	task, err := c.repo.Task.FindByID(payload.TaskID)
	if err != nil {
		return fmt.Errorf("查询任务失败: %w", err)
	}
	initialStage := c.analyzeInitialStage(task)
	claim, err := c.claimTaskForMessage(task.ID, TaskJobAnalyze, initialStage, payload.ClaimToken)
	if err != nil {
		return fmt.Errorf("获取分析 processing lease 失败: %w", err)
	}
	switch claim.Outcome {
	case repository.TaskLeaseBusy:
		return fmt.Errorf("分析 processing lease 正由其他消费者持有")
	case repository.TaskLeaseStale, repository.TaskLeaseTerminal:
		return nil
	case repository.TaskLeaseAcquired:
	default:
		return fmt.Errorf("未知分析 processing lease 状态: %s", claim.Outcome)
	}
	ctx, stopLease := c.startProcessingLeaseHeartbeat(ctx, task.ID, TaskJobAnalyze, claim.Token)
	defer stopLease()
	task.TraceID = traceIDForTask(payload.TraceID, task)
	ctx = c.contextForTaskJob(ctx, task, TaskJobAnalyze, payload.BudgetID)

	// 第 5 步：核心业务
	if err := c.processVideo(ctx, task); err != nil {
		if updateErr := c.recordTaskFailure(payload.TaskID, TaskJobAnalyze, "", err, claim.Token); updateErr != nil {
			return fmt.Errorf("任务失败且状态更新失败: %w", updateErr)
		}
		return nil
	}

	// 第 6 步：更新状态为已完成
	completed, err := c.completeTaskProcessing(repository.TaskProcessingCompleteRequest{TaskID: task.ID, JobType: TaskJobAnalyze, JobStage: model.TaskStageSummarizing, Token: claim.Token, TaskStatus: model.TaskStatusCompleted, TaskStage: model.TaskStageNone, Now: c.currentTime()})
	if err != nil {
		return fmt.Errorf("更新完成状态失败: %w", err)
	}
	if !completed {
		return nil
	}
	observability.Log(ctx, slog.Default(), slog.LevelInfo, "analyze task completed")
	return nil
}

func (c *Consumer) summarizeTask(ctx context.Context, task *model.VideoTask) error {
	ctx = observability.WithCorrelation(ctx, observability.Correlation{Stage: model.TaskStageSummarizing})
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	if err := c.transitionTaskStage(ctx, task.ID, model.TaskStageSummarizing); err != nil {
		return fmt.Errorf("更新总结阶段失败: %w", err)
	}
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	transcription, err := c.repo.Transcription.FindByTaskID(task.ID)
	if err != nil {
		return fmt.Errorf("查询转录失败: %w", err)
	}
	if transcription == nil || strings.TrimSpace(transcription.Content) == "" {
		return fmt.Errorf("缺少转录文本，无法生成 AI 总结")
	}

	if err := c.generateTitle(ctx, task, transcription.Content); err != nil {
		return err
	}

	observability.Log(ctx, slog.Default(), slog.LevelInfo, "ai summary started")
	taskAI, err := c.strategyForTask(task)
	if err != nil {
		return err
	}
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	summary, summarizeErr := taskAI.Summarize(ctx, transcription.Content)
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	if summarizeErr != nil {
		return fmt.Errorf("AI 总结失败: %w", summarizeErr)
	}

	if err := c.runLeasedSideEffect(ctx, func(repos *repository.Repositories) error {
		return repos.Summary.Upsert(&model.AISummary{
			TaskID: task.ID, Content: summary, ModelName: "mimo-v2.5",
		})
	}); err != nil {
		return fmt.Errorf("保存总结失败: %w", err)
	}
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}

	return nil
}

// generateTitle 在转写文本就绪后调用 LLM 生成简洁视频标题并写回。
// 失败仅记录日志，不阻塞转写/索引/总结主流程。
func (c *Consumer) generateTitle(ctx context.Context, task *model.VideoTask, transcript string) error {
	ctx = observability.WithCorrelation(ctx, observability.Correlation{Stage: "title_generation"})
	transcript = strings.TrimSpace(transcript)
	if transcript == "" || strings.TrimSpace(task.Title) != "" {
		return nil
	}
	if c.aiFactory == nil || c.profiles == nil {
		return nil
	}
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	profile, err := c.profiles.GetDefaultAIProfile(task.UserID)
	if err != nil || profile == nil {
		observability.Log(ctx, slog.Default(), slog.LevelWarn, "video title skipped: ai profile unavailable", slog.String("error", observability.SafeError(err)))
		return nil
	}
	chatClient, err := c.aiFactory.NewChatClient(*profile)
	if err != nil {
		observability.Log(ctx, slog.Default(), slog.LevelWarn, "video title skipped: chat client unavailable", slog.String("error", observability.SafeError(err)))
		return nil
	}
	chatClient = ai.NewObservedChatClient(chatClient, c.aiRecorder, ai.CallContext{
		UserID:      task.UserID,
		TaskID:      task.ID,
		LLMProvider: profile.LLMProvider,
		LLMModel:    profile.LLMModel,
		Kind:        model.AICallKindLLM,
	})

	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	title, chatErr := chatClient.Chat(ctx, []ai.ChatMessage{
		{Role: "system", Content: titleSystemPrompt},
		{Role: "user", Content: truncateRunes(transcript, 1000)},
	})
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	if chatErr != nil {
		observability.Log(ctx, slog.Default(), slog.LevelWarn, "video title generation failed", slog.String("error", observability.SafeError(chatErr)))
		return nil
	}
	title = sanitizeVideoTitle(title)
	if title == "" {
		return nil
	}
	if err := c.runLeasedSideEffect(ctx, func(repos *repository.Repositories) error {
		return repos.Task.UpdateTitle(task.ID, title)
	}); err != nil {
		if errors.Is(err, ErrProcessingLeaseLost) {
			return err
		}
		observability.Log(ctx, slog.Default(), slog.LevelError, "persist video title failed", slog.String("error", observability.SafeError(err)))
		return nil
	}
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	task.Title = title
	observability.Log(ctx, slog.Default(), slog.LevelInfo, "video title generated")
	return nil
}

func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// sanitizeVideoTitle 规整 LLM 返回的标题：去首尾空白与引号、合并换行、限长。
func sanitizeVideoTitle(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"'")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > 60 {
		s = string(r[:60])
	}
	return strings.TrimSpace(s)
}

const titleSystemPrompt = `根据用户提供的视频语音转写文本，生成一个简洁准确的视频标题。

要求：
1. 使用中文，不超过 30 个字。
2. 概括视频核心主题，客观中性，不要标题党或夸张表述。
3. 只输出标题文本本身，不要引号、序号、前缀（如"标题："）、换行或任何解释。
4. 若文本过短或无实质内容，输出"未命名视频"。`
