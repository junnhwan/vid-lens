package mq

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
	"gorm.io/gorm"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/observability"
	"vid-lens/internal/pkg/ffmpeg"
	"vid-lens/internal/pkg/lock"
	"vid-lens/internal/pkg/processingguard"
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

type kafkaMessageReader interface {
	FetchMessage(ctx context.Context) (kafka.Message, error)
	CommitMessages(ctx context.Context, messages ...kafka.Message) error
	Close() error
}

type kafkaReaderFactory func(config kafka.ReaderConfig) kafkaMessageReader
type kafkaMessageHandler func(ctx context.Context, message kafka.Message) error

// Consumer Kafka 消费者
// 面试亮点（消费端设计）：
//  1. 消费者组：同一个 Group 下的多个消费者分摊不同分区的消息，天然负载均衡
//  2. 基于 MD5 的 Key 路由：同一视频的消息一定进入同一分区，同一分区被同一消费者消费
//     → 保证了同一个视频不会被两个消费者同时处理（配合分布式锁双重保障）
//  3. 手动提交 offset：业务成功、失败已可靠移交 RetryScheduler，或毒消息已持久化隔离后才 commit
type Consumer struct {
	repo                   *repository.Repositories
	storage                *storage.MinIOStorage
	ai                     ai.Strategy
	aiFactory              *ai.Factory
	aiRecorder             ai.CallRecorder
	profiles               profileResolver
	rdb                    redis.Cmdable
	ffmpegPath             string
	ytdlpPath              string
	cookiesPath            string
	proxyURL               string
	splitAudio             splitAudioFunc
	ragIndex               ragIndexFunc
	ragProducer            ragIndexProducer
	retryPolicy            TaskRetryPolicy
	processingLease        time.Duration
	leaseHeartbeatInterval time.Duration
	now                    func() time.Time
	newToken               func() string

	newKafkaReader       kafkaReaderFactory
	readerRestartBackoff time.Duration

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
		repo:            repo,
		storage:         storage,
		ai:              aiStrategy,
		rdb:             rdb,
		ffmpegPath:      ffmpegPath,
		splitAudio:      ffmpeg.SplitAudio,
		processingLease: 30 * time.Minute,
		now:             time.Now,
		newToken:        uuid.NewString,
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

func groupReaderConfig(brokers []string, topic, groupID string) kafka.ReaderConfig {
	return kafka.ReaderConfig{
		Brokers:        brokers,
		Topic:          topic,
		GroupID:        groupID,
		MinBytes:       1e3,
		MaxBytes:       1e6,
		CommitInterval: 0,
		ReadBackoffMin: 100 * time.Millisecond,
		ReadBackoffMax: time.Second,
	}
}

func (c *Consumer) readerFactory() kafkaReaderFactory {
	if c.newKafkaReader != nil {
		return c.newKafkaReader
	}
	return func(config kafka.ReaderConfig) kafkaMessageReader {
		return kafka.NewReader(config)
	}
}

func (c *Consumer) restartBackoff() time.Duration {
	if c.readerRestartBackoff > 0 {
		return c.readerRestartBackoff
	}
	return time.Second
}

func consumeReader(ctx context.Context, reader kafkaMessageReader, handler kafkaMessageHandler) (err error) {
	defer func() {
		if closeErr := reader.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("关闭 Kafka reader 失败: %w", closeErr))
		}
	}()

	for {
		message, fetchErr := reader.FetchMessage(ctx)
		if fetchErr != nil {
			return fmt.Errorf("获取 Kafka 消息失败: %w", fetchErr)
		}
		if handleErr := handler(ctx, message); handleErr != nil {
			return fmt.Errorf("处理 Kafka 消息失败: %w", handleErr)
		}
		if commitErr := reader.CommitMessages(ctx, message); commitErr != nil {
			return fmt.Errorf("提交 Kafka offset 失败: %w", commitErr)
		}
	}
}

func (c *Consumer) runGroupConsumer(ctx context.Context, name string, config kafka.ReaderConfig, handler kafkaMessageHandler) {
	for ctx.Err() == nil {
		reader := c.readerFactory()(config)
		observability.Log(ctx, slog.Default(), slog.LevelInfo, "kafka consumer started", slog.String("consumer", name), slog.String("topic", config.Topic), slog.String("group", config.GroupID))
		err := consumeReader(ctx, reader, handler)
		if ctx.Err() != nil {
			return
		}
		observability.Log(ctx, slog.Default(), slog.LevelWarn, "kafka consumer rebuilding reader", slog.String("consumer", name), slog.String("error", observability.SafeError(err)))

		timer := time.NewTimer(c.restartBackoff())
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}

// Group consumers use FetchMessage and explicitly commit only after the handler
// either completes the business operation or durably records its failure for RetryScheduler.
// Any fetch, handler, or commit error closes this reader; the outer loop rebuilds it after backoff.
func (c *Consumer) poisonAwareHandler(name, groupID string, handler kafkaMessageHandler) kafkaMessageHandler {
	return func(ctx context.Context, message kafka.Message) error {
		err := handler(ctx, message)
		if err == nil || !isPoisonMessageError(err) {
			return err
		}
		if c == nil || c.repo == nil || c.repo.TaskMessageFailure == nil {
			return fmt.Errorf("poison 消息隔离仓储未初始化: %w", err)
		}
		failure := &model.KafkaMessageFailure{
			ConsumerGroup: groupID, ConsumerName: name, Topic: message.Topic,
			Partition: message.Partition, MessageOffset: message.Offset,
			MessageKey: append([]byte(nil), message.Key...), Payload: append([]byte(nil), message.Value...),
			ErrorMessage: truncateError(err),
		}
		if persistErr := c.repo.TaskMessageFailure.Record(failure); persistErr != nil {
			return fmt.Errorf("持久化 poison 消息失败: %w", persistErr)
		}
		return nil
	}
}

func isPoisonMessageError(err error) bool {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true
	}
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return true
	}
	var typeErr *json.UnmarshalTypeError
	return errors.As(err, &typeErr)
}

func (c *Consumer) startGroupConsumer(name string, brokers []string, topic, groupID string, handler kafkaMessageHandler) {
	durableHandler := c.poisonAwareHandler(name, groupID, handler)
	observedHandler := func(ctx context.Context, message kafka.Message) error {
		startedAt := time.Now()
		err := durableHandler(ctx, message)
		if metrics := observability.DefaultMetrics(); metrics != nil {
			metrics.ObserveKafkaJob(name, time.Since(startedAt))
		}
		return err
	}
	go c.runGroupConsumer(context.Background(), name, groupReaderConfig(brokers, topic, groupID), observedHandler)
}

func (c *Consumer) StartAnalyzeConsumer(brokers []string, topic, groupID string) {
	c.startGroupConsumer("analyze", brokers, topic, groupID, c.handleAnalyze)
}

func (c *Consumer) StartTranscribeConsumer(brokers []string, topic, groupID string) {
	c.startGroupConsumer("transcribe", brokers, topic, groupID, c.handleTranscribe)
}

func (c *Consumer) StartDownloadConsumer(brokers []string, topic, groupID string) {
	c.startGroupConsumer("download", brokers, topic, groupID, c.handleDownload)
}

func (c *Consumer) StartRAGIndexConsumer(brokers []string, topic, groupID string) {
	c.startGroupConsumer("rag_index", brokers, topic, groupID, c.handleRAGIndex)
}

func (c *Consumer) currentTime() time.Time {
	if c != nil && c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *Consumer) claimTaskForMessage(taskID int64, jobType, stage, messageToken string) (repository.TaskLeaseClaim, error) {
	if c == nil || c.repo == nil {
		return repository.TaskLeaseClaim{}, fmt.Errorf("任务仓储未初始化")
	}
	now := time.Now()
	if c.now != nil {
		now = c.now()
	}
	lease := c.processingLease
	if lease <= 0 {
		lease = 30 * time.Minute
	}
	newToken := uuid.NewString()
	if c.newToken != nil {
		newToken = c.newToken()
	}
	return c.repo.ClaimTaskProcessing(repository.TaskProcessingClaimRequest{
		TaskID: taskID, JobType: jobType, Stage: stage, MessageToken: messageToken,
		Now: now, LeaseUntil: now.Add(lease), NewToken: newToken,
	})
}

func retrySchedulerOwns(task *model.VideoTask) bool {
	return task != nil && task.Status == model.TaskStatusFailed && task.NextRetryAt != nil
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
	claim, err := c.claimTaskForMessage(task.ID, TaskJobDownload, model.TaskStageDownloading, payload.ClaimToken)
	if err != nil {
		return fmt.Errorf("获取下载 processing lease 失败: %w", err)
	}
	switch claim.Outcome {
	case repository.TaskLeaseBusy:
		return fmt.Errorf("下载 processing lease 正由其他消费者持有")
	case repository.TaskLeaseStale, repository.TaskLeaseTerminal:
		return nil
	case repository.TaskLeaseAcquired:
	default:
		return fmt.Errorf("未知下载 processing lease 状态: %s", claim.Outcome)
	}
	ctx, stopLease := c.startProcessingLeaseHeartbeat(ctx, task.ID, TaskJobDownload, claim.Token)
	defer stopLease()
	task.TraceID = traceID
	ctx = c.contextForTaskJob(ctx, task, TaskJobDownload, payload.BudgetID)
	if strings.TrimSpace(task.SourceURL) == "" {
		return c.recordTaskFailure(task.ID, TaskJobDownload, model.TaskStageDownloading, fmt.Errorf("URL 下载任务缺少 source_url"), claim.Token)
	}

	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	observability.Log(ctx, slog.Default(), slog.LevelInfo, "video download started")
	localPath, err := c.callDownloadVideo(ctx, task.SourceURL)
	if err != nil {
		errMsg := truncateError(err)
		observability.Log(ctx, slog.Default(), slog.LevelError, "video download failed", slog.String("error", observability.SafeError(err)))
		return c.recordTaskFailure(task.ID, TaskJobDownload, model.TaskStageDownloading, errors.New(errMsg), claim.Token)
	}
	defer os.Remove(localPath)

	fileMD5, size, err := hashLocalFile(localPath)
	if err != nil {
		return c.recordTaskFailure(task.ID, TaskJobDownload, model.TaskStageDownloading, err, claim.Token)
	}

	asset, err := c.repo.Asset.FindByMD5(fileMD5)
	if err != nil {
		return c.recordTaskFailure(task.ID, TaskJobDownload, model.TaskStageDownloading, err, claim.Token)
	}
	if asset == nil {
		objectName := fmt.Sprintf("videos/%s%s", uuid.New().String(), extensionForDownloadedFile(localPath))
		if err := c.callUploadLocalFile(ctx, localPath, objectName, "video/mp4"); err != nil {
			return c.recordTaskFailure(task.ID, TaskJobDownload, model.TaskStageDownloading, fmt.Errorf("上传到 MinIO 失败: %w", err), claim.Token)
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
				return c.recordTaskFailure(task.ID, TaskJobDownload, model.TaskStageDownloading, err, claim.Token)
			}
		}
	}

	filename := "WEB_" + filepath.Base(localPath)
	if filename == "WEB_" || filename == "WEB_." {
		filename = task.Filename
	}
	completed, err := c.completeTaskProcessing(repository.TaskProcessingCompleteRequest{
		TaskID: task.ID, JobType: TaskJobDownload, JobStage: model.TaskStageDownloading, Token: claim.Token,
		TaskStatus: model.TaskStatusPending, TaskStage: model.TaskStageUploaded, Now: c.currentTime(),
		TaskFields: map[string]interface{}{"asset_id": asset.ID, "file_md5": asset.FileMD5, "filename": filename, "file_url": asset.ObjectName, "file_size": asset.FileSize, "last_job_type": ""},
	})
	if err != nil {
		return fmt.Errorf("回写下载任务失败: %w", err)
	}
	if !completed {
		return nil
	}
	observability.Log(ctx, slog.Default(), slog.LevelInfo, "video download completed", slog.Int64("asset_id", asset.ID), slog.Int64("file_size", asset.FileSize))
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
	claim, err := c.claimTaskForMessage(task.ID, TaskJobRAGIndex, model.TaskStageIndexing, payload.ClaimToken)
	if err != nil {
		return fmt.Errorf("获取 RAG processing lease 失败: %w", err)
	}
	switch claim.Outcome {
	case repository.TaskLeaseBusy:
		return fmt.Errorf("RAG processing lease 正由其他消费者持有")
	case repository.TaskLeaseStale, repository.TaskLeaseTerminal:
		return nil
	case repository.TaskLeaseAcquired:
	default:
		return fmt.Errorf("未知 RAG processing lease 状态: %s", claim.Outcome)
	}
	ctx, stopLease := c.startProcessingLeaseHeartbeat(ctx, task.ID, TaskJobRAGIndex, claim.Token)
	defer stopLease()
	task.TraceID = traceID
	ctx = c.contextForTaskJob(ctx, task, TaskJobRAGIndex, payload.BudgetID)

	transcription, err := c.repo.Transcription.FindByTaskID(task.ID)
	if err != nil {
		return c.recordTaskFailure(task.ID, TaskJobRAGIndex, model.TaskStageIndexing, err, claim.Token)
	}
	if transcription == nil || strings.TrimSpace(transcription.Content) == "" {
		return c.recordTaskFailure(task.ID, TaskJobRAGIndex, model.TaskStageIndexing, fmt.Errorf("缺少转录文本，无法构建 RAG 索引"), claim.Token)
	}
	if c.ragIndex == nil {
		return c.recordTaskFailure(task.ID, TaskJobRAGIndex, model.TaskStageIndexing, fmt.Errorf("RAG 索引器未初始化"), claim.Token)
	}

	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	observability.Log(ctx, slog.Default(), slog.LevelInfo, "rag index started")
	ctx = processingguard.With(ctx, requireProcessingLease)
	ragErr := c.ragIndex(ctx, task)
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	if ragErr != nil {
		observability.Log(ctx, slog.Default(), slog.LevelError, "rag index failed", slog.String("error", observability.SafeError(ragErr)))
		return c.recordTaskFailure(task.ID, TaskJobRAGIndex, model.TaskStageIndexing, ragErr, claim.Token)
	}
	completed, err := c.completeTaskProcessing(repository.TaskProcessingCompleteRequest{
		TaskID: task.ID, JobType: TaskJobRAGIndex, JobStage: model.TaskStageIndexing, Token: claim.Token,
		TaskStatus: model.TaskStatusCompleted, TaskStage: model.TaskStageNone, Now: c.currentTime(),
	})
	if err != nil {
		return fmt.Errorf("完成 RAG processing lease 失败: %w", err)
	}
	if !completed {
		return nil
	}
	observability.Log(ctx, slog.Default(), slog.LevelInfo, "rag index completed")
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
	claim, err := c.claimTaskForMessage(task.ID, TaskJobTranscribe, model.TaskStageTranscribing, payload.ClaimToken)
	if err != nil {
		return fmt.Errorf("获取转录 processing lease 失败: %w", err)
	}
	switch claim.Outcome {
	case repository.TaskLeaseBusy:
		return fmt.Errorf("转录 processing lease 正由其他消费者持有")
	case repository.TaskLeaseStale, repository.TaskLeaseTerminal:
		return nil
	case repository.TaskLeaseAcquired:
	default:
		return fmt.Errorf("未知转录 processing lease 状态: %s", claim.Outcome)
	}
	ctx, stopLease := c.startProcessingLeaseHeartbeat(ctx, task.ID, TaskJobTranscribe, claim.Token)
	defer stopLease()
	task.TraceID = traceIDForTask(payload.TraceID, task)
	ctx = c.contextForTaskJob(ctx, task, TaskJobTranscribe, payload.BudgetID)

	videoPath, err := c.storage.DownloadToTemp(ctx, task.FileURL)
	if err != nil {
		return c.recordTaskFailure(payload.TaskID, TaskJobTranscribe, model.TaskStageTranscribing, err, claim.Token)
	}
	defer os.Remove(videoPath)

	audioPath, err := ffmpeg.ExtractAudio(ctx, c.ffmpegPath, videoPath)
	if err != nil {
		return c.recordTaskFailure(payload.TaskID, TaskJobTranscribe, model.TaskStageTranscribing, err, claim.Token)
	}
	defer os.Remove(audioPath)

	taskAI, err := c.strategyForTask(task)
	if err != nil {
		return c.recordTaskFailure(payload.TaskID, TaskJobTranscribe, model.TaskStageTranscribing, err, claim.Token)
	}

	transcript, err := c.transcribeAudio(ctx, task.ID, audioPath, taskAI)
	if err != nil {
		return c.recordTaskFailure(payload.TaskID, TaskJobTranscribe, model.TaskStageTranscribing, err, claim.Token)
	}

	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	if err := c.runLeasedSideEffect(ctx, func(repos *repository.Repositories) error {
		return repos.Transcription.Upsert(&model.VideoTranscription{
			TaskID: task.ID, Content: transcript, Words: len([]rune(transcript)),
		})
	}); err != nil {
		return c.recordTaskFailure(payload.TaskID, TaskJobTranscribe, model.TaskStageTranscribing, err, claim.Token)
	}
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	if err := c.indexAfterTranscription(ctx, task); err != nil {
		return err
	}
	if err := c.generateTitle(ctx, task, transcript); err != nil {
		return err
	}
	parentStatus, parentStage := int8(model.TaskStatusCompleted), model.TaskStageNone
	if c.ragProducer != nil {
		parentStatus, parentStage = model.TaskStatusRunning, model.TaskStageIndexing
	}
	completed, err := c.completeTaskProcessing(repository.TaskProcessingCompleteRequest{TaskID: task.ID, JobType: TaskJobTranscribe, JobStage: model.TaskStageTranscribing, Token: claim.Token, TaskStatus: parentStatus, TaskStage: parentStage, Now: c.currentTime()})
	if err != nil {
		return err
	}
	if !completed {
		return nil
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
		observability.Log(ctx, slog.Default(), slog.LevelInfo, "reuse transcription for summary")
		return c.summarizeTask(ctx, task)
	}

	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	if err := c.transitionTaskStage(ctx, task.ID, model.TaskStageTranscribing); err != nil {
		return fmt.Errorf("更新转录阶段失败: %w", err)
	}
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	observability.Log(ctx, slog.Default(), slog.LevelInfo, "audio extraction started")
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

	observability.Log(ctx, slog.Default(), slog.LevelInfo, "asr transcription started")
	taskAI, err := c.strategyForTask(task)
	if err != nil {
		return err
	}

	transcript, err := c.transcribeAudio(ctx, task.ID, audioPath, taskAI)
	if err != nil {
		return fmt.Errorf("语音转文字失败: %w", err)
	}

	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	if err := c.runLeasedSideEffect(ctx, func(repos *repository.Repositories) error {
		return repos.Transcription.Upsert(&model.VideoTranscription{
			TaskID: task.ID, Content: transcript, Words: len([]rune(transcript)),
		})
	}); err != nil {
		return fmt.Errorf("保存转录失败: %w", err)
	}
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	if err := c.indexAfterTranscription(ctx, task); err != nil {
		return err
	}

	return c.summarizeTask(ctx, task)
}

func (c *Consumer) transcribeAudio(ctx context.Context, taskID int64, audioPath string, strategy ai.Strategy) (string, error) {
	ctx = observability.WithCorrelation(ctx, observability.Correlation{Stage: model.TaskStageTranscribing})
	splitAudio := c.splitAudio
	if splitAudio == nil {
		splitAudio = ffmpeg.SplitAudio
	}

	observability.Log(ctx, slog.Default(), slog.LevelInfo, "asr chunking started", slog.Int64("task_id", taskID), slog.Int("segment_seconds", ffmpeg.DefaultAudioSegmentSeconds))
	chunks, err := splitAudio(ctx, c.ffmpegPath, audioPath, ffmpeg.DefaultAudioSegmentSeconds)
	if err != nil {
		return "", err
	}
	if err := requireProcessingLease(ctx); err != nil {
		return "", err
	}
	if len(chunks) == 0 {
		return "", fmt.Errorf("没有可转写的音频片段")
	}
	defer os.RemoveAll(filepath.Dir(chunks[0]))
	observability.Log(ctx, slog.Default(), slog.LevelInfo, "asr chunks prepared", slog.Int64("task_id", taskID), slog.Int("chunk_count", len(chunks)))

	parts := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		if err := requireProcessingLease(ctx); err != nil {
			return "", err
		}
		if completed := c.completedTranscriptionChunk(taskID, i); completed != "" {
			if metrics := observability.DefaultMetrics(); metrics != nil {
				metrics.IncASRChunkReuse()
			}
			observability.Log(ctx, slog.Default(), slog.LevelInfo, "asr chunk reused", slog.Int64("task_id", taskID), slog.Int("chunk_index", i+1), slog.Int("chunk_count", len(chunks)), slog.Int("output_chars", len([]rune(completed))))
			parts = append(parts, completed)
			continue
		}

		if err := c.markTranscriptionChunkRunning(ctx, taskID, i, chunk); err != nil {
			return "", err
		}
		if err := requireProcessingLease(ctx); err != nil {
			return "", err
		}
		chunkStartedAt := time.Now()
		text, transcribeErr := strategy.Transcribe(ctx, chunk)
		if err := requireProcessingLease(ctx); err != nil {
			return "", err
		}
		if transcribeErr != nil {
			if metrics := observability.DefaultMetrics(); metrics != nil {
				metrics.ObserveASRChunk("failed", time.Since(chunkStartedAt))
			}
			if err := c.markTranscriptionChunkFailed(ctx, taskID, i, chunk, transcribeErr); err != nil {
				return "", err
			}
			if err := requireProcessingLease(ctx); err != nil {
				return "", err
			}
			return "", fmt.Errorf("第 %d 段 ASR 失败: %w", i+1, transcribeErr)
		}
		if metrics := observability.DefaultMetrics(); metrics != nil {
			metrics.ObserveASRChunk("success", time.Since(chunkStartedAt))
		}
		text = strings.TrimSpace(text)
		chars := len([]rune(text))
		observability.Log(ctx, slog.Default(), slog.LevelInfo, "asr chunk completed", slog.Int64("task_id", taskID), slog.Int("chunk_index", i+1), slog.Int("chunk_count", len(chunks)), slog.Int("output_chars", chars))
		if text != "" {
			if err := c.markTranscriptionChunkCompleted(ctx, taskID, i, chunk, text); err != nil {
				return "", err
			}
			if err := requireProcessingLease(ctx); err != nil {
				return "", err
			}
			parts = append(parts, text)
		}
	}
	if err := requireProcessingLease(ctx); err != nil {
		return "", err
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("ASR 返回空结果")
	}

	transcript := strings.Join(parts, "\n\n")
	observability.Log(ctx, slog.Default(), slog.LevelInfo, "asr transcription completed", slog.Int64("task_id", taskID), slog.Int("chunk_count", len(chunks)), slog.Int("output_chars", len([]rune(transcript))))
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

func (c *Consumer) markTranscriptionChunkRunning(ctx context.Context, taskID int64, chunkIndex int, audioObject string) error {
	if c.repo == nil || c.repo.TranscriptionChunk == nil {
		return nil
	}
	return c.runLeasedSideEffect(ctx, func(repos *repository.Repositories) error {
		return repos.TranscriptionChunk.UpsertRunning(taskID, chunkIndex, audioObject)
	})
}

func (c *Consumer) markTranscriptionChunkCompleted(ctx context.Context, taskID int64, chunkIndex int, audioObject, content string) error {
	if c.repo == nil || c.repo.TranscriptionChunk == nil {
		return nil
	}
	return c.runLeasedSideEffect(ctx, func(repos *repository.Repositories) error {
		return repos.TranscriptionChunk.UpsertCompleted(taskID, chunkIndex, audioObject, content)
	})
}

func (c *Consumer) markTranscriptionChunkFailed(ctx context.Context, taskID int64, chunkIndex int, audioObject string, cause error) error {
	if c.repo == nil || c.repo.TranscriptionChunk == nil {
		return nil
	}
	return c.runLeasedSideEffect(ctx, func(repos *repository.Repositories) error {
		return repos.TranscriptionChunk.UpsertFailed(taskID, chunkIndex, audioObject, cause.Error())
	})
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

func (c *Consumer) indexAfterTranscription(ctx context.Context, task *model.VideoTask) error {
	if c.ragProducer == nil {
		return nil
	}
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	var budgetID string
	if c.repo != nil && c.repo.TaskJob != nil {
		if err := c.repo.TaskJob.UpsertQueued(task, TaskJobRAGIndex, model.TaskStageIndexing, task.MaxRetries); err != nil {
			observability.Log(ctx, slog.Default(), slog.LevelError, "persist rag index job state failed", slog.String("error", observability.SafeError(err)))
			return fmt.Errorf("persist rag index job state: %w", err)
		}
		if err := requireProcessingLease(ctx); err != nil {
			return err
		}
		var err error
		budgetID, err = c.repo.EnsureTaskJobRetryBudget(task.ID, TaskJobRAGIndex, c.currentTime())
		if err != nil {
			return fmt.Errorf("persist rag index retry budget: %w", err)
		}
	}
	ctx = ContextWithRetryBudgetID(ContextWithTraceID(ctx, task.TraceID), budgetID)
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	enqueueErr := c.ragProducer.EnqueueRAGIndex(ctx, task.ID)
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	if enqueueErr != nil {
		observability.Log(ctx, slog.Default(), slog.LevelError, "enqueue rag index failed", slog.String("error", observability.SafeError(enqueueErr)))
		c.recordRAGIndexEnqueueFailure(task, enqueueErr)

		owner := processingLeaseOwnerFromContext(ctx)
		if owner != nil && c.repo != nil {
			now := c.currentTime()
			policy := c.retryPolicy.normalized()
			nextRetryAt := now.Add(policy.backoffForRetry(1))
			currentStage := task.Stage
			if job, findErr := c.repo.TaskJob.FindByTaskAndType(task.ID, owner.jobType); findErr == nil && job != nil && job.Stage != "" {
				currentStage = job.Stage
			}
			updated, handoffErr := c.repo.FailTaskProcessingHandoff(repository.TaskProcessingHandoffFailureRequest{
				TaskID: task.ID, CurrentJobType: owner.jobType, CurrentStage: currentStage,
				NextJobType: TaskJobRAGIndex, NextStage: model.TaskStageIndexing, Token: owner.token,
				Status: model.TaskStatusFailed, ErrorCode: "enqueue_failed", ErrorMessage: truncateError(enqueueErr),
				RetryCount: 1, MaxRetries: policy.MaxRetries, NextRetryAt: &nextRetryAt, Now: now,
			})
			if handoffErr != nil {
				return fmt.Errorf("persist rag enqueue failure handoff: %w", handoffErr)
			}
			if !updated {
				return ErrProcessingLeaseLost
			}
		} else if c.repo != nil && c.repo.TaskJob != nil {
			// Compatibility for direct/non-consumer invocations: persist the child
			// failure, but only a processing owner may mutate the parent workflow.
			_ = c.repo.TaskJob.RecordTerminalFailure(task.ID, TaskJobRAGIndex, model.TaskStageIndexing, "enqueue_failed", truncateError(enqueueErr), 1, policyMaxRetries(c.retryPolicy), model.TaskStatusFailed)
		}
		return fmt.Errorf("enqueue rag index: %w", enqueueErr)
	}
	observability.Log(ctx, slog.Default(), slog.LevelInfo, "rag index enqueued")
	return nil
}

func policyMaxRetries(policy TaskRetryPolicy) int {
	return policy.normalized().MaxRetries
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

func (c *Consumer) contextForTaskJob(ctx context.Context, task *model.VideoTask, jobType string, payloadBudgetID ...string) context.Context {
	if c == nil || c.repo == nil || c.repo.TaskJob == nil || task == nil {
		return contextForTaskJob(ctx, task, nil)
	}
	job, err := c.repo.TaskJob.FindByTaskAndType(task.ID, jobType)
	if err != nil {
		observability.Log(ctx, slog.Default(), slog.LevelError, "load task job correlation failed",
			slog.String("job_type", jobType), slog.String("error", observability.SafeError(err)))
		return contextForTaskJob(ctx, task, nil)
	}
	ctx = contextForTaskJob(ctx, task, job)
	budgetID := ""
	if job != nil {
		budgetID = strings.TrimSpace(job.RetryBudgetID)
	}
	payloadID := ""
	if len(payloadBudgetID) > 0 {
		payloadID = strings.TrimSpace(payloadBudgetID[0])
	}
	if budgetID == "" {
		budgetID = payloadID
	} else if payloadID != "" && payloadID != budgetID {
		// The database binding is authoritative. A stale Kafka delivery must not
		// replace the retry cycle currently owned by the task job.
		observability.Log(ctx, slog.Default(), slog.LevelWarn, "stale retry budget in Kafka payload ignored",
			slog.String("job_type", jobType))
	}
	return ai.WithGovernanceContext(ctx, ai.GovernanceContext{
		RetryBudgetID: budgetID,
		Subject:       fmt.Sprintf("user:%d", task.UserID),
	})
}

func (c *Consumer) analyzeInitialStage(task *model.VideoTask) string {
	if task == nil {
		return model.TaskStageTranscribing
	}
	if task.Stage == model.TaskStageTranscribing || task.Stage == model.TaskStageSummarizing {
		return task.Stage
	}
	if c != nil && c.repo != nil && c.repo.Transcription != nil {
		if transcription, err := c.repo.Transcription.FindByTaskID(task.ID); err == nil && transcription != nil && strings.TrimSpace(transcription.Content) != "" {
			return model.TaskStageSummarizing
		}
	}
	return model.TaskStageTranscribing
}

func (c *Consumer) transitionTaskStage(ctx context.Context, taskID int64, nextStage string) error {
	if c == nil || c.repo == nil || c.repo.Task == nil {
		return fmt.Errorf("任务仓储未初始化")
	}
	task, err := c.repo.Task.FindByID(taskID)
	if err != nil {
		return err
	}
	if task.Stage == nextStage {
		return nil
	}
	finishedAt := c.currentTime()
	startedAt := finishedAt
	if task.StageStartedAt != nil {
		startedAt = *task.StageStartedAt
	}
	if err := c.runLeasedSideEffect(ctx, func(repos *repository.Repositories) error {
		return repos.Task.UpdateStatusAndStage(taskID, model.TaskStatusRunning, nextStage, "")
	}); err != nil {
		return err
	}
	if task.Stage != "" && task.Stage != model.TaskStageNone && task.Stage != model.TaskStageUploaded {
		if startedAt.After(finishedAt) {
			startedAt = finishedAt
		}
		if metrics := observability.DefaultMetrics(); metrics != nil {
			metrics.ObserveTaskStage(task.Stage, "success", finishedAt.Sub(startedAt))
		}
	}
	return nil
}

func (c *Consumer) completeTaskProcessing(req repository.TaskProcessingCompleteRequest) (bool, error) {
	if c == nil || c.repo == nil {
		return false, fmt.Errorf("任务仓储未初始化")
	}
	startedAt := req.Now
	if task, err := c.repo.Task.FindByID(req.TaskID); err == nil && task != nil && task.StageStartedAt != nil {
		startedAt = *task.StageStartedAt
	}
	completed, err := c.repo.CompleteTaskProcessing(req)
	if err != nil || !completed {
		return completed, err
	}
	finishedAt := req.Now
	if finishedAt.IsZero() {
		finishedAt = c.currentTime()
	}
	if startedAt.IsZero() || startedAt.After(finishedAt) {
		startedAt = finishedAt
	}
	if metrics := observability.DefaultMetrics(); metrics != nil {
		metrics.ObserveTaskStage(req.JobStage, "success", finishedAt.Sub(startedAt))
	}
	return true, nil
}

func (c *Consumer) markTaskJobRunning(task *model.VideoTask, jobType, stage string) {
	if c == nil || c.repo == nil || c.repo.TaskJob == nil || task == nil {
		return
	}
	if err := c.repo.TaskJob.UpsertDispatching(task, jobType, model.TaskStatusRunning, stage); err != nil {
		observability.Log(contextForTaskJob(context.Background(), task, nil), slog.Default(), slog.LevelError, "persist task job state failed", slog.String("job_type", jobType), slog.String("error", observability.SafeError(err)))
		return
	}
	if err := c.repo.TaskJob.MarkRunning(task.ID, jobType, stage); err != nil {
		observability.Log(contextForTaskJob(context.Background(), task, nil), slog.Default(), slog.LevelError, "persist task job running state failed", slog.String("job_type", jobType), slog.String("error", observability.SafeError(err)))
	}
}

func (c *Consumer) markTaskJobCompleted(taskID int64, jobType, stage string) {
	if c == nil || c.repo == nil || c.repo.TaskJob == nil {
		return
	}
	startedAt := time.Now()
	if job, err := c.repo.TaskJob.FindByTaskAndType(taskID, jobType); err == nil && job != nil && job.StartedAt != nil {
		startedAt = *job.StartedAt
	}
	if err := c.repo.TaskJob.MarkCompleted(taskID, jobType, stage); err != nil {
		observability.Log(context.Background(), slog.Default(), slog.LevelError, "persist task job completed state failed", slog.Int64("task_id", taskID), slog.String("job_type", jobType), slog.String("error", observability.SafeError(err)))
		return
	}
	if metrics := observability.DefaultMetrics(); metrics != nil {
		metrics.ObserveTaskStage(stage, "success", time.Since(startedAt))
	}
}
