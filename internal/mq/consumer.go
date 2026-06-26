package mq

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/ffmpeg"
	"vid-lens/internal/pkg/lock"
	"vid-lens/internal/pkg/ytdlp"
	"vid-lens/internal/repository"
	"vid-lens/internal/storage"
)

type splitAudioFunc func(ctx context.Context, ffmpegPath, inputPath string, segmentSeconds int) ([]string, error)
type ragIndexFunc func(ctx context.Context, task *model.VideoTask) error
type downloadVideoFunc func(ctx context.Context, sourceURL string) (string, error)
type uploadLocalFileFunc func(ctx context.Context, localPath, objectName, contentType string) error
type ragIndexProducer interface {
	EnqueueRAGIndex(ctx context.Context, taskID int64) error
}

// Consumer Kafka 消费者
// 面试亮点（消费端设计）：
//  1. 消费者组：同一个 Group 下的多个消费者分摊不同分区的消息，天然负载均衡
//  2. 基于 MD5 的 Key 路由：同一视频的消息一定进入同一分区，同一分区被同一消费者消费
//     → 保证了同一个视频不会被两个消费者同时处理（配合分布式锁双重保障）
//  3. 手动提交 offset：只有业务逻辑执行成功才 commit，防止消息丢失
type Consumer struct {
	repo        *repository.Repositories
	storage     *storage.MinIOStorage
	ai          ai.Strategy
	aiFactory   *ai.Factory
	aiRecorder  ai.CallRecorder
	profiles    profileResolver
	rdb         redis.Cmdable
	ffmpegPath  string
	ytdlpPath   string
	cookiesPath string
	proxyURL    string
	splitAudio  splitAudioFunc
	ragIndex    ragIndexFunc
	ragProducer ragIndexProducer
	retryPolicy TaskRetryPolicy

	downloadVideo   downloadVideoFunc
	uploadLocalFile uploadLocalFileFunc
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
	consumer := &Consumer{
		repo:       repo,
		storage:    storage,
		ai:         aiStrategy,
		rdb:        rdb,
		ffmpegPath: ffmpegPath,
		splitAudio: ffmpeg.SplitAudio,
	}
	consumer.uploadLocalFile = func(ctx context.Context, localPath, objectName, contentType string) error {
		if consumer.storage == nil {
			return fmt.Errorf("对象存储未初始化")
		}
		_, err := consumer.storage.UploadFromPath(ctx, localPath, objectName, contentType)
		return err
	}
	return consumer
}

func (c *Consumer) SetDownloadTools(ytdlpPath, ffmpegPath, cookiesPath, proxyURL string) {
	c.ytdlpPath = ytdlpPath
	c.cookiesPath = cookiesPath
	c.proxyURL = proxyURL
	if ffmpegPath != "" {
		c.ffmpegPath = ffmpegPath
	}
}

func (c *Consumer) SetAIResolver(factory *ai.Factory, profiles profileResolver) {
	c.aiFactory = factory
	c.profiles = profiles
}

func (c *Consumer) SetAIRecorder(recorder ai.CallRecorder) {
	c.aiRecorder = recorder
}

func (c *Consumer) SetRAGIndexer(indexer ragIndexFunc) {
	c.ragIndex = indexer
}

func (c *Consumer) SetRAGIndexProducer(producer ragIndexProducer) {
	c.ragProducer = producer
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

func (c *Consumer) StartDownloadConsumer(brokers []string, topic, groupID string) {
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        brokers,
		Topic:          topic,
		GroupID:        groupID,
		CommitInterval: 0,
		MinBytes:       1e3,
		MaxBytes:       1e6,
	})

	go func() {
		log.Println("✅ Kafka 消费者已启动 [download]")
		for {
			msg, err := r.ReadMessage(context.Background())
			if err != nil {
				log.Printf("[Kafka] 读取下载消息失败: %v", err)
				time.Sleep(time.Second)
				continue
			}

			// 与 analyze/transcribe 一致：只有业务成功才 commit offset。
			// 业务级失败（下载失败、上传失败等）已由 handleDownload 记入 task_job 表，
			// 由 RetryScheduler 兜底，此时返回 nil 走 commit；
			// 基础设施级失败（消息解析、DB 查询、回写）返回 err，不 commit，由 Kafka at-least-once 重投。
			if err := c.handleDownload(context.Background(), msg); err != nil {
				log.Printf("[Kafka] 下载任务消息异常（不提交 offset，等待重投）: %v", err)
			} else {
				if err := r.CommitMessages(context.Background(), msg); err != nil {
					log.Printf("[Kafka] download commit offset 失败: %v", err)
				}
			}
		}
	}()
}

func (c *Consumer) StartRAGIndexConsumer(brokers []string, topic, groupID string) {
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        brokers,
		Topic:          topic,
		GroupID:        groupID,
		CommitInterval: 0,
		MinBytes:       1e3,
		MaxBytes:       1e6,
	})

	go func() {
		log.Println("✅ Kafka 消费者已启动 [rag_index]")
		for {
			msg, err := r.ReadMessage(context.Background())
			if err != nil {
				log.Printf("[Kafka] 读取 RAG 索引消息失败: %v", err)
				time.Sleep(time.Second)
				continue
			}

			// 同 download：业务成功才 commit；基础设施级失败返回 err 不 commit，等待 Kafka 重投。
			if err := c.handleRAGIndex(context.Background(), msg); err != nil {
				log.Printf("[Kafka] RAG 索引任务消息异常（不提交 offset，等待重投）: %v", err)
			} else {
				if err := r.CommitMessages(context.Background(), msg); err != nil {
					log.Printf("[Kafka] rag_index commit offset 失败: %v", err)
				}
			}
		}
	}()
}

func (c *Consumer) handleDownload(ctx context.Context, msg kafka.Message) error {
	var payload DownloadPayload
	if err := json.Unmarshal(msg.Value, &payload); err != nil {
		return fmt.Errorf("解析下载消息失败: %w", err)
	}

	task, err := c.repo.Task.FindByID(payload.TaskID)
	if err != nil {
		return fmt.Errorf("查询下载任务失败: %w", err)
	}
	traceID := traceIDForTask(payload.TraceID, task)
	if task.Status != model.TaskStatusRunning || task.Stage != model.TaskStageDownloading {
		log.Printf("[Kafka] 下载任务状态已变化，跳过: traceID=%s taskID=%d status=%d stage=%s", traceID, task.ID, task.Status, task.Stage)
		return nil
	}
	c.markTaskJobRunning(task, TaskJobDownload, model.TaskStageDownloading)
	if strings.TrimSpace(task.SourceURL) == "" {
		_ = c.recordTaskFailure(task.ID, TaskJobDownload, model.TaskStageDownloading, fmt.Errorf("URL 下载任务缺少 source_url"))
		return nil
	}

	log.Printf("[Kafka] URL 下载开始: traceID=%s taskID=%d url=%s", traceID, task.ID, sanitizeURLForLog(task.SourceURL))
	localPath, err := c.callDownloadVideo(ctx, task.SourceURL)
	if err != nil {
		errMsg := truncateError(err)
		log.Printf("[Kafka] URL 下载失败: traceID=%s taskID=%d userID=%d url=%s err=%v", traceID, task.ID, task.UserID, sanitizeURLForLog(task.SourceURL), err)
		_ = c.recordTaskFailure(task.ID, TaskJobDownload, model.TaskStageDownloading, errors.New(errMsg))
		return nil
	}
	defer os.Remove(localPath)

	fileMD5, size, err := hashLocalFile(localPath)
	if err != nil {
		_ = c.recordTaskFailure(task.ID, TaskJobDownload, model.TaskStageDownloading, err)
		return nil
	}

	asset, err := c.repo.Asset.FindByMD5(fileMD5)
	if err != nil {
		_ = c.recordTaskFailure(task.ID, TaskJobDownload, model.TaskStageDownloading, err)
		return nil
	}
	if asset == nil {
		objectName := fmt.Sprintf("videos/%s%s", uuid.New().String(), extensionForDownloadedFile(localPath))
		if err := c.callUploadLocalFile(ctx, localPath, objectName, "video/mp4"); err != nil {
			_ = c.recordTaskFailure(task.ID, TaskJobDownload, model.TaskStageDownloading, fmt.Errorf("上传到 MinIO 失败: %w", err))
			return nil
		}
		asset = &model.VideoAsset{
			FileMD5:     fileMD5,
			ObjectName:  objectName,
			FileSize:    size,
			ContentType: "video/mp4",
		}
		if err := c.repo.Asset.Create(asset); err != nil {
			existing, findErr := c.repo.Asset.FindByMD5(fileMD5)
			if findErr == nil && existing != nil {
				asset = existing
			} else {
				_ = c.recordTaskFailure(task.ID, TaskJobDownload, model.TaskStageDownloading, err)
				return nil
			}
		}
	}

	filename := "WEB_" + filepath.Base(localPath)
	if filename == "WEB_" || filename == "WEB_." {
		filename = task.Filename
	}
	if err := c.repo.Task.CompleteURLDownload(task.ID, asset, filename, time.Now()); err != nil {
		return fmt.Errorf("回写下载任务失败: %w", err)
	}
	c.markTaskJobCompleted(task.ID, TaskJobDownload, model.TaskStageDownloading)
	log.Printf("[Kafka] URL 下载完成: traceID=%s taskID=%d assetID=%d md5=%s size=%d", traceID, task.ID, asset.ID, asset.FileMD5, asset.FileSize)
	return nil
}

func (c *Consumer) handleRAGIndex(ctx context.Context, msg kafka.Message) error {
	var payload RAGIndexPayload
	if err := json.Unmarshal(msg.Value, &payload); err != nil {
		return fmt.Errorf("解析 RAG 索引消息失败: %w", err)
	}

	task, err := c.repo.Task.FindByID(payload.TaskID)
	if err != nil {
		return fmt.Errorf("查询 RAG 索引任务失败: %w", err)
	}
	traceID := traceIDForTask(payload.TraceID, task)
	ctx = ContextWithTraceID(ctx, traceID)
	c.markTaskJobRunning(task, TaskJobRAGIndex, model.TaskStageIndexing)

	transcription, err := c.repo.Transcription.FindByTaskID(task.ID)
	if err != nil {
		_ = c.recordTaskFailure(task.ID, TaskJobRAGIndex, model.TaskStageIndexing, err)
		return nil
	}
	if transcription == nil || strings.TrimSpace(transcription.Content) == "" {
		_ = c.recordTaskFailure(task.ID, TaskJobRAGIndex, model.TaskStageIndexing, fmt.Errorf("缺少转录文本，无法构建 RAG 索引"))
		return nil
	}
	if c.ragIndex == nil {
		_ = c.recordTaskFailure(task.ID, TaskJobRAGIndex, model.TaskStageIndexing, fmt.Errorf("RAG 索引器未初始化"))
		return nil
	}

	log.Printf("[Kafka] RAG 索引任务开始: traceID=%s taskID=%d userID=%d", traceID, task.ID, task.UserID)
	if err := c.ragIndex(ctx, task); err != nil {
		log.Printf("[Kafka] RAG 索引任务失败: traceID=%s taskID=%d err=%v", traceID, task.ID, err)
		_ = c.recordTaskFailure(task.ID, TaskJobRAGIndex, model.TaskStageIndexing, err)
		return nil
	}
	c.markTaskJobCompleted(task.ID, TaskJobRAGIndex, model.TaskStageIndexing)
	log.Printf("[Kafka] RAG 索引任务完成: traceID=%s taskID=%d", traceID, task.ID)
	return nil
}

func (c *Consumer) callDownloadVideo(ctx context.Context, sourceURL string) (string, error) {
	if c.downloadVideo != nil {
		return c.downloadVideo(ctx, sourceURL)
	}
	return ytdlp.DownloadVideo(ctx, c.ytdlpPath, c.ffmpegPath, c.cookiesPath, c.proxyURL, sourceURL)
}

func (c *Consumer) callUploadLocalFile(ctx context.Context, localPath, objectName, contentType string) error {
	if c.uploadLocalFile != nil {
		return c.uploadLocalFile(ctx, localPath, objectName, contentType)
	}
	return fmt.Errorf("对象存储未初始化")
}

// handleAnalyze 处理视频分析任务
// 严格遵循六步流程（对应面试文档 MQ 消费者开发规范）
func (c *Consumer) handleAnalyze(ctx context.Context, msg kafka.Message) error {
	// 第 1 步：解析消息
	var payload AnalyzePayload
	if err := json.Unmarshal(msg.Value, &payload); err != nil {
		return fmt.Errorf("解析消息失败: %w", err)
	}

	log.Printf("[Kafka] 收到分析任务: traceID=%s taskID=%d, md5=%s", payload.TraceID, payload.TaskID, payload.MD5)

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
	updated, err := c.repo.Task.UpdateStatusAndStageIf(payload.TaskID,
		[]int8{model.TaskStatusPending, model.TaskStatusQueued, model.TaskStatusFailed, model.TaskStatusCompleted},
		model.TaskStatusRunning, model.TaskStageSummarizing, "")
	if err != nil {
		return fmt.Errorf("更新任务状态失败: %w", err)
	}
	if !updated {
		log.Printf("[Kafka] 任务状态已变化，跳过: taskID=%d", payload.TaskID)
		return nil
	}
	c.markTaskJobRunning(task, TaskJobAnalyze, model.TaskStageSummarizing)

	// 第 5 步：核心业务
	if err := c.processVideo(ctx, task); err != nil {
		if updateErr := c.recordTaskFailure(payload.TaskID, TaskJobAnalyze, "", err); updateErr != nil {
			return fmt.Errorf("任务失败且状态更新失败: %w", updateErr)
		}
		return nil
	}

	// 第 6 步：更新状态为已完成
	if err := c.repo.Task.UpdateStatusAndStage(payload.TaskID, model.TaskStatusCompleted, model.TaskStageNone, ""); err != nil {
		return fmt.Errorf("更新完成状态失败: %w", err)
	}
	c.markTaskJobCompleted(payload.TaskID, TaskJobAnalyze, model.TaskStageSummarizing)
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

	updated, err := c.repo.Task.UpdateStatusAndStageIf(payload.TaskID,
		[]int8{model.TaskStatusPending, model.TaskStatusQueued, model.TaskStatusFailed, model.TaskStatusCompleted},
		model.TaskStatusRunning, model.TaskStageTranscribing, "")
	if err != nil {
		return err
	}
	if !updated {
		return nil
	}
	c.markTaskJobRunning(task, TaskJobTranscribe, model.TaskStageTranscribing)

	videoPath, err := c.storage.DownloadToTemp(ctx, task.FileURL)
	if err != nil {
		_ = c.recordTaskFailure(payload.TaskID, TaskJobTranscribe, model.TaskStageTranscribing, err)
		return nil
	}
	defer os.Remove(videoPath)

	audioPath, err := ffmpeg.ExtractAudio(ctx, c.ffmpegPath, videoPath)
	if err != nil {
		_ = c.recordTaskFailure(payload.TaskID, TaskJobTranscribe, model.TaskStageTranscribing, err)
		return nil
	}
	defer os.Remove(audioPath)

	taskAI, err := c.strategyForTask(task)
	if err != nil {
		_ = c.recordTaskFailure(payload.TaskID, TaskJobTranscribe, model.TaskStageTranscribing, err)
		return nil
	}

	transcript, err := c.transcribeAudio(ctx, task.ID, audioPath, taskAI)
	if err != nil {
		_ = c.recordTaskFailure(payload.TaskID, TaskJobTranscribe, model.TaskStageTranscribing, err)
		return nil
	}

	if err := c.repo.Transcription.Upsert(&model.VideoTranscription{
		TaskID:  task.ID,
		Content: transcript,
		Words:   len([]rune(transcript)),
	}); err != nil {
		_ = c.recordTaskFailure(payload.TaskID, TaskJobTranscribe, model.TaskStageTranscribing, err)
		return nil
	}
	_ = c.repo.Task.UpdateStatusAndStage(payload.TaskID, model.TaskStatusRunning, model.TaskStageIndexing, "")
	c.indexAfterTranscription(ctx, task)

	if err := c.repo.Task.UpdateStatusAndStage(payload.TaskID, model.TaskStatusCompleted, model.TaskStageNone, ""); err != nil {
		return err
	}
	c.markTaskJobCompleted(payload.TaskID, TaskJobTranscribe, model.TaskStageTranscribing)
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

	_ = c.repo.Task.UpdateStatusAndStage(task.ID, model.TaskStatusRunning, model.TaskStageTranscribing, "")
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
	_ = c.repo.Task.UpdateStatusAndStage(task.ID, model.TaskStatusRunning, model.TaskStageIndexing, "")
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
		if completed := c.completedTranscriptionChunk(taskID, i); completed != "" {
			log.Printf("[Kafka] 音频切片转写片段复用: taskID=%d, chunk=%d/%d, path=%s, chars=%d", taskID, i+1, len(chunks), chunk, len([]rune(completed)))
			parts = append(parts, completed)
			continue
		}
		c.markTranscriptionChunkRunning(taskID, i, chunk)
		text, err := strategy.Transcribe(ctx, chunk)
		if err != nil {
			c.markTranscriptionChunkFailed(taskID, i, chunk, err)
			return "", fmt.Errorf("第 %d 段 ASR 失败: %w", i+1, err)
		}
		text = strings.TrimSpace(text)
		chars := len([]rune(text))
		log.Printf("[Kafka] 音频切片转写片段完成: taskID=%d, chunk=%d/%d, path=%s, chars=%d", taskID, i+1, len(chunks), chunk, chars)
		if text != "" {
			c.markTranscriptionChunkCompleted(taskID, i, chunk, text)
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

func (c *Consumer) completedTranscriptionChunk(taskID int64, chunkIndex int) string {
	if c.repo == nil || c.repo.TranscriptionChunk == nil {
		return ""
	}
	chunk, err := c.repo.TranscriptionChunk.FindByTaskAndIndex(taskID, chunkIndex)
	if err != nil || chunk == nil {
		return ""
	}
	if chunk.Status == model.TranscriptionChunkStatusCompleted && strings.TrimSpace(chunk.Content) != "" {
		return strings.TrimSpace(chunk.Content)
	}
	return ""
}

func (c *Consumer) markTranscriptionChunkRunning(taskID int64, chunkIndex int, audioObject string) {
	if c.repo == nil || c.repo.TranscriptionChunk == nil {
		return
	}
	if err := c.repo.TranscriptionChunk.UpsertRunning(taskID, chunkIndex, audioObject); err != nil {
		log.Printf("[Kafka] 转写分片状态写入失败: taskID=%d chunk=%d err=%v", taskID, chunkIndex+1, err)
	}
}

func (c *Consumer) markTranscriptionChunkCompleted(taskID int64, chunkIndex int, audioObject, content string) {
	if c.repo == nil || c.repo.TranscriptionChunk == nil {
		return
	}
	if err := c.repo.TranscriptionChunk.UpsertCompleted(taskID, chunkIndex, audioObject, content); err != nil {
		log.Printf("[Kafka] 转写分片完成状态写入失败: taskID=%d chunk=%d err=%v", taskID, chunkIndex+1, err)
	}
}

func (c *Consumer) markTranscriptionChunkFailed(taskID int64, chunkIndex int, audioObject string, err error) {
	if c.repo == nil || c.repo.TranscriptionChunk == nil {
		return
	}
	if writeErr := c.repo.TranscriptionChunk.UpsertFailed(taskID, chunkIndex, audioObject, err.Error()); writeErr != nil {
		log.Printf("[Kafka] 转写分片失败状态写入失败: taskID=%d chunk=%d err=%v", taskID, chunkIndex+1, writeErr)
	}
}

func (c *Consumer) strategyForTask(task *model.VideoTask) (ai.Strategy, error) {
	if c.profiles == nil || c.aiFactory == nil {
		if c.ai == nil {
			return nil, fmt.Errorf("请先配置 AI 服务")
		}
		return ai.NewObservedStrategy(c.ai, c.aiRecorder, ai.CallContext{
			UserID: task.UserID,
			TaskID: task.ID,
		}), nil
	}

	profile, err := c.profiles.GetDefaultAIProfile(task.UserID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, fmt.Errorf("请先配置 AI 服务")
	}
	strategy, err := c.aiFactory.NewAnalysisStrategy(*profile)
	if err != nil {
		return nil, err
	}
	return ai.NewObservedStrategy(strategy, c.aiRecorder, ai.CallContext{
		UserID:      task.UserID,
		TaskID:      task.ID,
		ASRProvider: profile.ASRProvider,
		ASRModel:    profile.ASRModel,
		LLMProvider: profile.LLMProvider,
		LLMModel:    profile.LLMModel,
	}), nil
}

func (c *Consumer) summarizeTask(ctx context.Context, task *model.VideoTask) error {
	_ = c.repo.Task.UpdateStatusAndStage(task.ID, model.TaskStatusRunning, model.TaskStageSummarizing, "")
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
	if c.ragProducer == nil {
		return
	}
	if c.repo != nil && c.repo.TaskJob != nil {
		if err := c.repo.TaskJob.UpsertQueued(task, TaskJobRAGIndex, model.TaskStageIndexing, task.MaxRetries); err != nil {
			log.Printf("[Kafka] RAG 索引子任务状态写入失败: taskID=%d err=%v", task.ID, err)
		}
	}
	ctx = ContextWithTraceID(ctx, task.TraceID)
	if err := c.ragProducer.EnqueueRAGIndex(ctx, task.ID); err != nil {
		log.Printf("[Kafka] RAG 索引任务投递失败，可稍后手动重试: taskID=%d, err=%v", task.ID, err)
		if c.repo != nil && c.repo.TaskJob != nil {
			_ = c.repo.TaskJob.RecordTerminalFailure(task.ID, TaskJobRAGIndex, model.TaskStageIndexing, "enqueue_failed", truncateError(err), task.RetryCount, task.MaxRetries, model.TaskStatusFailed)
		}
		c.recordRAGIndexEnqueueFailure(task, err)
		return
	}
	log.Printf("[Kafka] RAG 索引任务已投递: taskID=%d", task.ID)
}

func (c *Consumer) recordRAGIndexEnqueueFailure(task *model.VideoTask, err error) {
	if c.repo == nil || c.repo.RAGIndex == nil || c.profiles == nil || task == nil {
		return
	}
	profile, profileErr := c.profiles.GetDefaultAIProfile(task.UserID)
	if profileErr != nil || profile == nil || strings.TrimSpace(profile.EmbeddingModel) == "" {
		return
	}
	now := time.Now()
	errMsg := truncateError(fmt.Errorf("RAG 索引任务投递失败: %w", err))
	_ = c.repo.RAGIndex.Upsert(&model.VideoRAGIndex{
		UserID:         task.UserID,
		TaskID:         task.ID,
		EmbeddingModel: profile.EmbeddingModel,
		EmbeddingDim:   profile.EmbeddingDim,
		Status:         model.RAGIndexStatusFailed,
		ChunkCount:     0,
		LastError:      errMsg,
		BuildVersion:   1,
		StartedAt:      &now,
		FinishedAt:     &now,
	})
}

func truncateError(err error) string {
	errMsg := err.Error()
	if len(errMsg) > 500 {
		return errMsg[:500]
	}
	return errMsg
}

func hashLocalFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("打开下载文件失败: %w", err)
	}
	defer file.Close()

	hasher := md5.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return "", 0, fmt.Errorf("计算下载文件 MD5 失败: %w", err)
	}
	if size == 0 {
		return "", 0, fmt.Errorf("下载文件为空")
	}
	return hex.EncodeToString(hasher.Sum(nil)), size, nil
}

func sanitizeURLForLog(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "<invalid-url>"
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func extensionForDownloadedFile(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".avi", ".mkv", ".mov", ".webm", ".mp4":
		return ext
	default:
		return ".mp4"
	}
}

func traceIDForTask(payloadTraceID string, task *model.VideoTask) string {
	if payloadTraceID != "" {
		return payloadTraceID
	}
	if task != nil {
		return task.TraceID
	}
	return ""
}

func (c *Consumer) markTaskJobRunning(task *model.VideoTask, jobType, stage string) {
	if c == nil || c.repo == nil || c.repo.TaskJob == nil || task == nil {
		return
	}
	if err := c.repo.TaskJob.UpsertDispatching(task, jobType, model.TaskStatusRunning, stage); err != nil {
		log.Printf("[Kafka] 子任务状态写入失败: taskID=%d jobType=%s err=%v", task.ID, jobType, err)
		return
	}
	if err := c.repo.TaskJob.MarkRunning(task.ID, jobType, stage); err != nil {
		log.Printf("[Kafka] 子任务运行状态写入失败: taskID=%d jobType=%s err=%v", task.ID, jobType, err)
	}
}

func (c *Consumer) markTaskJobCompleted(taskID int64, jobType, stage string) {
	if c == nil || c.repo == nil || c.repo.TaskJob == nil {
		return
	}
	if err := c.repo.TaskJob.MarkCompleted(taskID, jobType, stage); err != nil {
		log.Printf("[Kafka] 子任务完成状态写入失败: taskID=%d jobType=%s err=%v", taskID, jobType, err)
	}
}
