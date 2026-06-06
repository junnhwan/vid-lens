package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/ffmpeg"
	"vid-lens/internal/pkg/lock"
	"vid-lens/internal/repository"
	"vid-lens/internal/storage"
)

type splitAudioFunc func(ctx context.Context, ffmpegPath, inputPath string, segmentSeconds int) ([]string, error)
type ragIndexFunc func(ctx context.Context, task *model.VideoTask) error

// Consumer Kafka 消费者
// 面试亮点（消费端设计）：
//  1. 消费者组：同一个 Group 下的多个消费者分摊不同分区的消息，天然负载均衡
//  2. 基于 MD5 的 Key 路由：同一视频的消息一定进入同一分区，同一分区被同一消费者消费
//     → 保证了同一个视频不会被两个消费者同时处理（配合分布式锁双重保障）
//  3. 手动提交 offset：只有业务逻辑执行成功才 commit，防止消息丢失
type Consumer struct {
	repo       *repository.Repositories
	storage    *storage.MinIOStorage
	ai         ai.Strategy
	aiFactory  *ai.Factory
	profiles   profileResolver
	rdb        redis.Cmdable
	ffmpegPath string
	splitAudio splitAudioFunc
	ragIndex   ragIndexFunc
}

type profileResolver interface {
	GetDefaultAIProfile(userID int64) (*ai.Profile, error)
}

// NewConsumer 创建消费者
func NewConsumer(
	repo *repository.Repositories,
	storage *storage.MinIOStorage,
	aiStrategy ai.Strategy,
	rdb redis.Cmdable,
	ffmpegPath string,
) *Consumer {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	return &Consumer{
		repo:       repo,
		storage:    storage,
		ai:         aiStrategy,
		rdb:        rdb,
		ffmpegPath: ffmpegPath,
		splitAudio: ffmpeg.SplitAudio,
	}
}

func (c *Consumer) SetAIResolver(factory *ai.Factory, profiles profileResolver) {
	c.aiFactory = factory
	c.profiles = profiles
}

func (c *Consumer) SetRAGIndexer(indexer ragIndexFunc) {
	c.ragIndex = indexer
}

// StartAnalyzeConsumer 启动 AI 分析消费者
// 面试亮点：对应面试文档中 RocketMQ 的消费者监听模式
// Kafka 版本通过 Reader 按 Group 消费，自动管理 offset
func (c *Consumer) StartAnalyzeConsumer(brokers []string, topic, groupID string) {
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        brokers,
		Topic:          topic,
		GroupID:        groupID,
		MinBytes:       1e3, // 1KB
		MaxBytes:       1e6, // 1MB
		CommitInterval: 0,   // 手动提交（不自动提交）
		ReadBackoffMin: 100 * time.Millisecond,
		ReadBackoffMax: 1 * time.Second,
	})

	go func() {
		log.Println("✅ Kafka 消费者已启动 [analyze]")
		for {
			msg, err := r.ReadMessage(context.Background())
			if err != nil {
				log.Printf("[Kafka] 读取消息失败: %v", err)
				time.Sleep(time.Second)
				continue
			}

			if err := c.handleAnalyze(context.Background(), msg); err != nil {
				log.Printf("[Kafka] 分析任务失败: %v", err)
				// 面试亮点：消费失败不 commit offset，下次会重新消费
				// 这就是 Kafka 的 at-least-once 语义
				// 配合业务层的幂等校验（分布式锁 + 状态检查），不会重复执行
			} else {
				// 手动 commit：只有业务成功才提交 offset
				if err := r.CommitMessages(context.Background(), msg); err != nil {
					log.Printf("[Kafka] commit offset 失败: %v", err)
				}
			}
		}
	}()
}

// StartTranscribeConsumer 启动文字提取消费者
func (c *Consumer) StartTranscribeConsumer(brokers []string, topic, groupID string) {
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        brokers,
		Topic:          topic,
		GroupID:        groupID,
		CommitInterval: 0,
		MinBytes:       1e3,
		MaxBytes:       1e6,
	})

	go func() {
		log.Println("✅ Kafka 消费者已启动 [transcribe]")
		for {
			msg, err := r.ReadMessage(context.Background())
			if err != nil {
				log.Printf("[Kafka] 读取消息失败: %v", err)
				time.Sleep(time.Second)
				continue
			}

			if err := c.handleTranscribe(context.Background(), msg); err != nil {
				log.Printf("[Kafka] 转录任务失败: %v", err)
			} else {
				r.CommitMessages(context.Background(), msg)
			}
		}
	}()
}

// handleAnalyze 处理视频分析任务
// 严格遵循六步流程（对应面试文档 MQ 消费者开发规范）
func (c *Consumer) handleAnalyze(ctx context.Context, msg kafka.Message) error {
	// 第 1 步：解析消息
	var payload AnalyzePayload
	if err := json.Unmarshal(msg.Value, &payload); err != nil {
		return fmt.Errorf("解析消息失败: %w", err)
	}

	log.Printf("[Kafka] 收到分析任务: taskID=%d, md5=%s", payload.TaskID, payload.MD5)

	// 第 2 步：基于 MD5 获取分布式锁
	lockKey := fmt.Sprintf("vidlens:lock:%s", payload.MD5)
	distLock := lock.NewRedisLock(c.rdb, lockKey)

	acquired, err := distLock.TryLock(ctx, 5*time.Second)
	if err != nil {
		return fmt.Errorf("获取分布式锁失败: %w", err)
	}
	if !acquired {
		log.Printf("[Kafka] 抢锁失败，跳过: md5=%s", payload.MD5)
		return fmt.Errorf("同一视频正在处理中")
	}
	defer distLock.Unlock(ctx)

	// 第 3 步：幂等校验
	task, err := c.repo.Task.FindByID(payload.TaskID)
	if err != nil {
		return fmt.Errorf("查询任务失败: %w", err)
	}
	if task.Status == model.TaskStatusCompleted {
		summary, err := c.repo.Summary.FindByTaskID(task.ID)
		if err != nil {
			return fmt.Errorf("查询任务总结失败: %w", err)
		}
		if summary != nil {
			log.Printf("[Kafka] 任务已完成，跳过: taskID=%d", payload.TaskID)
			return nil
		}
		log.Printf("[Kafka] 任务已完成但缺少总结，继续分析: taskID=%d", payload.TaskID)
	}

	// 第 4 步：更新状态为处理中
	updated, err := c.repo.Task.UpdateStatusIf(payload.TaskID,
		[]int8{model.TaskStatusPending, model.TaskStatusQueued, model.TaskStatusFailed, model.TaskStatusCompleted},
		model.TaskStatusRunning, "")
	if err != nil {
		return fmt.Errorf("更新任务状态失败: %w", err)
	}
	if !updated {
		log.Printf("[Kafka] 任务状态已变化，跳过: taskID=%d", payload.TaskID)
		return nil
	}

	// 第 5 步：核心业务
	if err := c.processVideo(ctx, task); err != nil {
		errMsg := err.Error()
		if len(errMsg) > 500 {
			errMsg = errMsg[:500]
		}
		if updateErr := c.repo.Task.UpdateStatus(payload.TaskID, model.TaskStatusFailed, errMsg); updateErr != nil {
			return fmt.Errorf("任务失败且状态更新失败: %w", updateErr)
		}
		return nil
	}

	// 第 6 步：更新状态为已完成
	if err := c.repo.Task.UpdateStatus(payload.TaskID, model.TaskStatusCompleted, ""); err != nil {
		return fmt.Errorf("更新完成状态失败: %w", err)
	}
	log.Printf("[Kafka] 任务完成: taskID=%d", payload.TaskID)
	return nil
}

// handleTranscribe 处理文字提取任务
func (c *Consumer) handleTranscribe(ctx context.Context, msg kafka.Message) error {
	var payload AnalyzePayload
	if err := json.Unmarshal(msg.Value, &payload); err != nil {
		return err
	}

	task, err := c.repo.Task.FindByID(payload.TaskID)
	if err != nil {
		return err
	}

	updated, err := c.repo.Task.UpdateStatusIf(payload.TaskID,
		[]int8{model.TaskStatusPending, model.TaskStatusQueued, model.TaskStatusFailed, model.TaskStatusCompleted},
		model.TaskStatusRunning, "")
	if err != nil {
		return err
	}
	if !updated {
		return nil
	}

	videoPath, err := c.storage.DownloadToTemp(ctx, task.FileURL)
	if err != nil {
		_ = c.repo.Task.UpdateStatus(payload.TaskID, model.TaskStatusFailed, truncateError(err))
		return nil
	}
	defer os.Remove(videoPath)

	audioPath, err := ffmpeg.ExtractAudio(ctx, c.ffmpegPath, videoPath)
	if err != nil {
		_ = c.repo.Task.UpdateStatus(payload.TaskID, model.TaskStatusFailed, truncateError(err))
		return nil
	}
	defer os.Remove(audioPath)

	taskAI, err := c.strategyForTask(task)
	if err != nil {
		_ = c.repo.Task.UpdateStatus(payload.TaskID, model.TaskStatusFailed, truncateError(err))
		return nil
	}

	transcript, err := c.transcribeAudio(ctx, task.ID, audioPath, taskAI)
	if err != nil {
		_ = c.repo.Task.UpdateStatus(payload.TaskID, model.TaskStatusFailed, truncateError(err))
		return nil
	}

	if err := c.repo.Transcription.Upsert(&model.VideoTranscription{
		TaskID:  task.ID,
		Content: transcript,
		Words:   len([]rune(transcript)),
	}); err != nil {
		_ = c.repo.Task.UpdateStatus(payload.TaskID, model.TaskStatusFailed, truncateError(err))
		return nil
	}
	c.indexAfterTranscription(ctx, task)

	if err := c.repo.Task.UpdateStatus(payload.TaskID, model.TaskStatusCompleted, ""); err != nil {
		return err
	}
	return nil
}

// processVideo 核心业务：FFmpeg → ASR → LLM
func (c *Consumer) processVideo(ctx context.Context, task *model.VideoTask) error {
	existingTranscription, err := c.repo.Transcription.FindByTaskID(task.ID)
	if err != nil {
		return fmt.Errorf("查询转录失败: %w", err)
	}
	if existingTranscription != nil && strings.TrimSpace(existingTranscription.Content) != "" {
		log.Printf("[Kafka] 复用已有转录生成总结: taskID=%d", task.ID)
		return c.summarizeTask(ctx, task)
	}

	log.Printf("[Kafka] 提取音频: taskID=%d", task.ID)
	videoPath, err := c.storage.DownloadToTemp(ctx, task.FileURL)
	if err != nil {
		return fmt.Errorf("下载视频失败: %w", err)
	}
	defer os.Remove(videoPath)

	audioPath, err := ffmpeg.ExtractAudio(ctx, c.ffmpegPath, videoPath)
	if err != nil {
		return fmt.Errorf("提取音频失败: %w", err)
	}
	defer os.Remove(audioPath)

	log.Printf("[Kafka] ASR 转录: taskID=%d", task.ID)
	taskAI, err := c.strategyForTask(task)
	if err != nil {
		return err
	}

	transcript, err := c.transcribeAudio(ctx, task.ID, audioPath, taskAI)
	if err != nil {
		return fmt.Errorf("语音转文字失败: %w", err)
	}

	if err := c.repo.Transcription.Upsert(&model.VideoTranscription{
		TaskID:  task.ID,
		Content: transcript,
		Words:   len([]rune(transcript)),
	}); err != nil {
		return fmt.Errorf("保存转录失败: %w", err)
	}
	c.indexAfterTranscription(ctx, task)

	return c.summarizeTask(ctx, task)
}

func (c *Consumer) transcribeAudio(ctx context.Context, taskID int64, audioPath string, strategy ai.Strategy) (string, error) {
	splitAudio := c.splitAudio
	if splitAudio == nil {
		splitAudio = ffmpeg.SplitAudio
	}

	log.Printf("[Kafka] 音频切片转写开始: taskID=%d, path=%s, segmentSeconds=%d", taskID, audioPath, ffmpeg.DefaultAudioSegmentSeconds)
	chunks, err := splitAudio(ctx, c.ffmpegPath, audioPath, ffmpeg.DefaultAudioSegmentSeconds)
	if err != nil {
		return "", err
	}
	if len(chunks) == 0 {
		return "", fmt.Errorf("没有可转写的音频片段")
	}
	defer os.RemoveAll(filepath.Dir(chunks[0]))
	log.Printf("[Kafka] 音频切片转写已切片: taskID=%d, chunks=%d", taskID, len(chunks))

	parts := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		text, err := strategy.Transcribe(ctx, chunk)
		if err != nil {
			return "", fmt.Errorf("第 %d 段 ASR 失败: %w", i+1, err)
		}
		text = strings.TrimSpace(text)
		chars := len([]rune(text))
		log.Printf("[Kafka] 音频切片转写片段完成: taskID=%d, chunk=%d/%d, path=%s, chars=%d", taskID, i+1, len(chunks), chunk, chars)
		if text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("ASR 返回空结果")
	}

	transcript := strings.Join(parts, "\n\n")
	log.Printf("[Kafka] 音频切片转写完成: taskID=%d, chunks=%d, transcriptChars=%d", taskID, len(chunks), len([]rune(transcript)))
	return transcript, nil
}

func (c *Consumer) strategyForTask(task *model.VideoTask) (ai.Strategy, error) {
	if c.profiles == nil || c.aiFactory == nil {
		if c.ai == nil {
			return nil, fmt.Errorf("请先配置 AI 服务")
		}
		return c.ai, nil
	}

	profile, err := c.profiles.GetDefaultAIProfile(task.UserID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, fmt.Errorf("请先配置 AI 服务")
	}
	return c.aiFactory.NewAnalysisStrategy(*profile)
}

func (c *Consumer) summarizeTask(ctx context.Context, task *model.VideoTask) error {
	transcription, err := c.repo.Transcription.FindByTaskID(task.ID)
	if err != nil {
		return fmt.Errorf("查询转录失败: %w", err)
	}
	if transcription == nil || strings.TrimSpace(transcription.Content) == "" {
		return fmt.Errorf("缺少转录文本，无法生成 AI 总结")
	}

	log.Printf("[Kafka] AI 总结: taskID=%d", task.ID)
	taskAI, err := c.strategyForTask(task)
	if err != nil {
		return err
	}
	summary, err := taskAI.Summarize(ctx, transcription.Content)
	if err != nil {
		return fmt.Errorf("AI 总结失败: %w", err)
	}

	if err := c.repo.Summary.Upsert(&model.AISummary{
		TaskID:    task.ID,
		Content:   summary,
		ModelName: "mimo-v2.5",
	}); err != nil {
		return fmt.Errorf("保存总结失败: %w", err)
	}

	return nil
}

func (c *Consumer) indexAfterTranscription(ctx context.Context, task *model.VideoTask) {
	if c.ragIndex == nil {
		return
	}
	if err := c.ragIndex(ctx, task); err != nil {
		log.Printf("[Kafka] RAG 索引构建失败，可稍后手动重试: taskID=%d, err=%v", task.ID, err)
		return
	}
	log.Printf("[Kafka] RAG 索引构建完成: taskID=%d", task.ID)
}

func truncateError(err error) string {
	errMsg := err.Error()
	if len(errMsg) > 500 {
		return errMsg[:500]
	}
	return errMsg
}
