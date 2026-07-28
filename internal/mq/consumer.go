package mq

import (
	"context"
	"fmt"
	"sync"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/ffmpeg"
	"vid-lens/internal/pkg/remoteurl"
	"vid-lens/internal/repository"
	"vid-lens/internal/storage"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	amqp "github.com/rabbitmq/amqp091-go"
)

type splitAudioFunc func(ctx context.Context, ffmpegPath, inputPath string, segmentSeconds int) ([]string, error)

type ragIndexFunc func(ctx context.Context, task *model.VideoTask) error

type visualIndexFunc func(ctx context.Context, task *model.VideoTask) (int, error)

type downloadVideoFunc func(ctx context.Context, sourceURL string) (string, error)

type uploadLocalFileFunc func(ctx context.Context, localPath, objectName, contentType string) error

type ragIndexProducer interface {
	EnqueueRAGIndex(ctx context.Context, taskID int64) error
}

type messageReader interface {
	Consume(ctx context.Context) (<-chan amqp.Delivery, error)
	Close() error
}

type messageReaderFactory func(queue, groupID string) messageReader

type messageHandler func(ctx context.Context, delivery amqp.Delivery) error

// Consumer RabbitMQ 消费者
// 面试亮点（消费端设计）：
//  1. manual ack：业务成功后才 Ack；handler 失败 Nack(requeue=true) 触发 at-least-once 重投
//  2. 消费侧幂等键：同一 MessageId 重复投递由 Redis SETNX 挡住（amqp.Delivery.MessageId），
//     与 task 状态机 CAS 双重保障——重复消费不产生重复 ASR/索引烧 token
//  3. 毒消息隔离：unparseable 消息持久化到 kafka_message_failures 表后 Ack，
//     不阻塞队列；MaxRetries 耗尽的 job 由 DLX 路由到 video-<jobtype>-dlq
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
	downloadURLPolicy      remoteurl.Policy
	splitAudio             splitAudioFunc
	ragIndex               ragIndexFunc
	visualIndex            visualIndexFunc
	ragProducer            ragIndexProducer
	retryPolicy            TaskRetryPolicy
	processingLease        time.Duration
	leaseHeartbeatInterval time.Duration
	now                    func() time.Time
	newToken               func() string

	newMessageReader     messageReaderFactory
	readerRestartBackoff time.Duration
	amqpURL              string
	prefetch             int
	idempotency          idempotencyChecker

	downloadVideo   downloadVideoFunc
	uploadLocalFile uploadLocalFileFunc

	wg sync.WaitGroup
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
		repo:              repo,
		storage:           storage,
		ai:                aiStrategy,
		rdb:               rdb,
		ffmpegPath:        ffmpegPath,
		splitAudio:        ffmpeg.SplitAudio,
		processingLease:   30 * time.Minute,
		now:               time.Now,
		newToken:          uuid.NewString,
		downloadURLPolicy: remoteurl.NewPolicy(nil, nil),
		prefetch:          1,
		idempotency:       newRedisIdempotencyChecker(rdb, 0),
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

// SetDownloadURLPolicy configures the admission-time URL checks used by the
// real yt-dlp execution path. The policy is deliberately shared with the HTTP
// URL upload validator so queued messages are checked again at execution time.
func (c *Consumer) SetDownloadURLPolicy(allowedHosts []string, resolver remoteurl.Resolver) {
	c.downloadURLPolicy = remoteurl.NewPolicy(allowedHosts, resolver)
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

func (c *Consumer) SetVisualIndexer(indexer visualIndexFunc) {
	c.visualIndex = indexer
}

func (c *Consumer) SetRAGIndexProducer(producer ragIndexProducer) {
	c.ragProducer = producer
}

// SetMQConfig wires the RabbitMQ connection URL and prefetch from config. The
// consumer builds one connection+channel per queue lazily in runGroupConsumer.
func (c *Consumer) SetMQConfig(brokers []string, prefetch int) {
	if len(brokers) > 0 {
		c.amqpURL = "amqp://" + brokers[0]
	}
	if prefetch > 0 {
		c.prefetch = prefetch
	}
}
