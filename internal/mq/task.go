package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/ffmpeg"
	"vid-lens/internal/pkg/lock"
	"vid-lens/internal/repository"
	"vid-lens/internal/storage"
)

// 任务类型常量
const (
	TaskTypeAnalyze    = "video:analyze"     // 视频分析（ASR + AI 总结）
	TaskTypeTranscribe = "video:transcribe"  // 仅文字提取
)

// AnalyzePayload 任务消息载荷
type AnalyzePayload struct {
	TaskID int64  `json:"task_id"`
	MD5    string `json:"md5"`
}

// Worker 异步任务处理器
// 面试亮点（对比原项目使用 RocketMQ）：
//   选择 Asynq 的理由：
//   1. 基于 Redis（项目已引入），不增加额外基础设施
//   2. 原生支持指数退避重试 + 死信队列（Asynq 叫 Archive）
//   3. 原生支持任务去重（通过 TaskID）
//   4. Go 生态中做任务队列的最成熟方案
type Worker struct {
	repo    *repository.Repositories
	storage *storage.MinIOStorage
	ai      ai.Strategy
	rdb     redis.Cmdable
}

// NewWorker 创建任务处理器
func NewWorker(
	repo *repository.Repositories,
	storage *storage.MinIOStorage,
	aiStrategy ai.Strategy,
	rdb redis.Cmdable,
) *Worker {
	return &Worker{
		repo:    repo,
		storage: storage,
		ai:      aiStrategy,
		rdb:     rdb,
	}
}

// HandleAnalyze 处理视频分析任务
// 面试亮点：严格遵循六步流程（对应面试文档中的 MQ 消费者开发规范）
//   1. 解析消息 → 2. 分布式锁 → 3. 幂等校验 → 4. 核心业务 → 5. 更新状态 → 6. 释放锁
func (w *Worker) HandleAnalyze(ctx context.Context, t *asynq.Task) error {
	// 第 1 步：解析消息
	var payload AnalyzePayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("解析任务消息失败: %w", err)
	}

	log.Printf("[MQ] 收到分析任务: taskID=%d, md5=%s", payload.TaskID, payload.MD5)

	// 第 2 步：基于 MD5 获取分布式锁
	lockKey := fmt.Sprintf("vidlens:lock:%s", payload.MD5)
	distLock := lock.NewRedisLock(w.rdb, lockKey)

	acquired, err := distLock.TryLock(ctx, 5*time.Second)
	if err != nil {
		return fmt.Errorf("获取分布式锁失败: %w", err)
	}
	if !acquired {
		log.Printf("[MQ] 抢锁失败，任务可能已被其他消费者处理: md5=%s", payload.MD5)
		return nil // 不是错误，直接返回成功
	}
	defer distLock.Unlock(ctx)

	// 第 3 步：幂等校验 —— 查询任务状态，已完成则直接返回
	task, err := w.repo.Task.FindByID(payload.TaskID)
	if err != nil {
		return fmt.Errorf("查询任务失败: %w", err)
	}
	if task.Status == model.TaskStatusCompleted {
		log.Printf("[MQ] 任务已完成，跳过: taskID=%d", payload.TaskID)
		return nil
	}

	// 第 4 步：更新状态为处理中
	if err := w.repo.Task.UpdateStatus(payload.TaskID, model.TaskStatusRunning, ""); err != nil {
		return fmt.Errorf("更新任务状态失败: %w", err)
	}

	// 第 5 步：核心业务 —— FFmpeg 提取音频 → ASR → AI 总结
	if err := w.processVideo(ctx, task); err != nil {
		errMsg := err.Error()
		if len(errMsg) > 500 {
			errMsg = errMsg[:500]
		}
		w.repo.Task.UpdateStatus(payload.TaskID, model.TaskStatusFailed, errMsg)
		// 返回 error 触发 Asynq 重试
		return err
	}

	// 第 6 步：更新状态为已完成
	w.repo.Task.UpdateStatus(payload.TaskID, model.TaskStatusCompleted, "")
	log.Printf("[MQ] 任务完成: taskID=%d", payload.TaskID)
	return nil
}

// HandleTranscribe 处理纯文字提取任务
func (w *Worker) HandleTranscribe(ctx context.Context, t *asynq.Task) error {
	var payload AnalyzePayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("解析任务消息失败: %w", err)
	}

	task, err := w.repo.Task.FindByID(payload.TaskID)
	if err != nil {
		return err
	}

	// FFmpeg 提取音频
	audioPath, err := ffmpeg.ExtractAudio(ctx, "ffmpeg", task.FileURL)
	if err != nil {
		return err
	}
	defer os.Remove(audioPath)

	// ASR 转录
	transcript, err := w.ai.Transcribe(ctx, audioPath)
	if err != nil {
		return err
	}

	w.repo.Transcription.Upsert(&model.VideoTranscription{
		TaskID:  task.ID,
		Content: transcript,
		Words:   len([]rune(transcript)),
	})

	w.repo.Task.UpdateStatus(payload.TaskID, model.TaskStatusCompleted, "")
	return nil
}

// processVideo 核心业务：提取音频 → ASR → AI 总结
func (w *Worker) processVideo(ctx context.Context, task *model.VideoTask) error {
	videoPath := task.FileURL

	// 1. FFmpeg 提取音频
	log.Printf("[MQ] 提取音频: taskID=%d", task.ID)
	audioPath, err := ffmpeg.ExtractAudio(ctx, "ffmpeg", videoPath)
	if err != nil {
		return fmt.Errorf("提取音频失败: %w", err)
	}
	defer os.Remove(audioPath)

	// 2. ASR 语音转文字
	log.Printf("[MQ] ASR 转录: taskID=%d", task.ID)
	transcript, err := w.ai.Transcribe(ctx, audioPath)
	if err != nil {
		return fmt.Errorf("语音转文字失败: %w", err)
	}

	// 保存转录结果（垂直拆分到独立表）
	w.repo.Transcription.Upsert(&model.VideoTranscription{
		TaskID:  task.ID,
		Content: transcript,
		Words:   len([]rune(transcript)),
	})

	// 3. AI 智能总结
	log.Printf("[MQ] AI 总结: taskID=%d", task.ID)
	summary, err := w.ai.Summarize(ctx, transcript)
	if err != nil {
		return fmt.Errorf("AI 总结失败: %w", err)
	}

	// 保存总结结果（垂直拆分到独立表）
	w.repo.Summary.Upsert(&model.AISummary{
		TaskID:    task.ID,
		Content:   summary,
		ModelName: "DeepSeek-R1-Distill-Qwen-32B",
	})

	return nil
}
