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
	"github.com/segmentio/kafka-go"
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
	downloadURLPolicy      remoteurl.Policy
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

func (c *Consumer) SetRAGIndexProducer(producer ragIndexProducer) {
	c.ragProducer = producer
}
