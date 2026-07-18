package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
	"vid-lens/internal/ai"
	"vid-lens/internal/config"
	"vid-lens/internal/handler"
	"vid-lens/internal/middleware"
	"vid-lens/internal/model"
	"vid-lens/internal/mq"
	"vid-lens/internal/pkg/secret"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
	"vid-lens/internal/storage"
)

type serverDependencies struct {
	cfg               *config.Config
	repos             *repository.Repositories
	rdb               redis.Cmdable
	minioStorage      *storage.MinIOStorage
	producer          *mq.Producer
	providerAdmission *ai.QuotaAdmission
	ragStore          service.RAGVectorStore
	ragRetriever      service.RAGRetriever
}

type serverApplication struct {
	handlers             serverHandlers
	rateLimiter          *middleware.RateLimiter
	consumer             *mq.Consumer
	retryScheduler       *mq.RetryScheduler
	taskCleanup          *service.TaskCleanupService
	taskCleanupScheduler *service.TaskCleanupScheduler
	kafka                config.KafkaConfig
}

func (deps serverDependencies) validate(aiStrategy ai.Strategy) error {
	switch {
	case deps.cfg == nil:
		return fmt.Errorf("server config is nil")
	case deps.repos == nil:
		return fmt.Errorf("server repositories are nil")
	case deps.rdb == nil:
		return fmt.Errorf("server redis client is nil")
	case deps.minioStorage == nil:
		return fmt.Errorf("server minio storage is nil")
	case deps.producer == nil:
		return fmt.Errorf("server kafka producer is nil")
	case aiStrategy == nil:
		return fmt.Errorf("AI strategy is nil")
	default:
		return nil
	}
}

// wireServerApplication constructs services, handlers, consumers, and retry
// scheduling from already-initialized infrastructure. It deliberately does not
// open network connections or start goroutines; those lifecycle actions belong
// to Start and the caller's shutdown sequence.
func productionRetrievalConfig(cfg config.RAGConfig) service.RAGRetrievalConfig {
	retrieval := service.DefaultRAGRetrievalConfig()
	retrieval.QueryMode = service.QueryModeOriginal
	retrieval.RewriteQueries = 1
	retrieval.TopK = cfg.TopK
	retrieval.CandidateK = cfg.CandidateK
	retrieval.RRFK = 60
	retrieval.NeighborRadius = 0
	retrieval.MaxContextChars = 0
	retrieval.MinVectorScore = cfg.MinScore
	retrieval.RerankerMode = service.RerankerModeNone
	retrieval.RerankerVersion = ""
	return retrieval
}

func wireServerApplication(deps serverDependencies, aiStrategy ai.Strategy) (*serverApplication, error) {
	if err := deps.validate(aiStrategy); err != nil {
		return nil, err
	}
	apiKeySecret := deps.cfg.Security.APIKeySecret
	if apiKeySecret == "" {
		apiKeySecret = deps.cfg.JWT.Secret
		log.Println("⚠️ security.api_key_secret 未配置，临时复用 jwt.secret；公开部署请设置 VIDLENS_API_KEY_SECRET")
	}
	secretCodec, err := secret.NewCodecFromPassphrase(apiKeySecret)
	if err != nil {
		return nil, fmt.Errorf("初始化 API Key 加密器失败: %w", err)
	}

	aiFactory := ai.NewFactoryWithAdmission(deps.providerAdmission)
	userSvc := service.NewUserService(deps.repos.User, deps.cfg.JWT)
	aiProfileSvc := service.NewAIProfileService(deps.repos.AIProfile, secretCodec, &aiProfileTesterAdapter{tester: ai.NewProfileTester(aiFactory)})
	ragIndexSvc := service.NewRAGIndexService(deps.repos, deps.ragStore, service.RAGIndexConfig{
		ChunkSize:    deps.cfg.RAG.ChunkSize,
		ChunkOverlap: deps.cfg.RAG.ChunkOverlap,
		EmbeddingDim: deps.cfg.RAG.EmbeddingDim,
	})
	aiObserver := service.NewAIObserver(deps.repos)
	ragIndexSvc.SetAIRecorder(aiObserver)

	retrievalCfg := productionRetrievalConfig(deps.cfg.RAG)
	chatSvc := service.NewChatService(deps.repos, deps.ragRetriever, service.ChatConfig{
		TopK:        deps.cfg.RAG.TopK,
		CandidateK:  deps.cfg.RAG.CandidateK,
		MinScore:    deps.cfg.RAG.MinScore,
		RecentTurns: deps.cfg.RAG.RecentTurns,
		Retrieval:   &retrievalCfg,
	})
	chatSvc.SetAIRecorder(aiObserver)
	chatSvc.SetMemoryStore(service.NewRedisChatMemoryStore(deps.rdb))

	mediaSvc := service.NewMediaService(deps.repos, deps.minioStorage, deps.producer, deps.cfg.Upload, deps.cfg.Tools)
	uploadSessionSvc := service.NewUploadSessionService(deps.repos, deps.minioStorage, service.UploadSessionConfig{
		MaxFileSize:  deps.cfg.Upload.MaxFileSize,
		MaxChunkSize: deps.cfg.Upload.ChunkSize,
	})
	var vectorCleaner service.TaskVectorCleaner
	if deps.ragStore != nil {
		vectorCleaner = deps.ragStore
	}
	taskCleanup := service.NewTaskCleanupService(
		deps.repos,
		deps.minioStorage,
		vectorCleaner,
		service.TaskCleanupConfig{
			LeaseDuration: time.Duration(deps.cfg.Cleanup.LeaseSeconds) * time.Second,
			RetryBackoff:  time.Duration(deps.cfg.Cleanup.RetryBackoffSeconds) * time.Second,
		},
	)
	mediaSvc.SetTaskCleanupService(taskCleanup)
	taskCleanupScheduler := service.NewTaskCleanupScheduler(taskCleanup, service.TaskCleanupSchedulerConfig{
		BatchSize: deps.cfg.Cleanup.BatchSize,
		Interval:  time.Duration(deps.cfg.Cleanup.ScanIntervalSeconds) * time.Second,
	})

	rateLimiter := middleware.NewRateLimiter(deps.rdb, deps.cfg.RateLimit.Capacity, deps.cfg.RateLimit.Rate)
	// 高成本 AI 接口按路由单独配更严格的限额（覆盖全局默认）
	for path, route := range deps.cfg.RateLimit.Routes {
		rateLimiter.SetRouteLimit(path, route.Capacity, route.Rate)
	}

	consumer := mq.NewConsumer(deps.repos, deps.minioStorage, aiStrategy, deps.rdb, deps.cfg.Tools.FFmpegPath)
	consumer.SetDownloadTools(deps.cfg.Tools.YtDlpPath, deps.cfg.Tools.FFmpegPath, deps.cfg.Tools.CookiesPath, deps.cfg.Tools.ProxyURL)
	consumer.SetDownloadURLPolicy(deps.cfg.Tools.AllowedVideoHosts, nil)
	consumer.SetRetryPolicy(mq.TaskRetryPolicy{
		MaxRetries:     deps.cfg.TaskRetry.MaxRetries,
		BackoffSeconds: deps.cfg.TaskRetry.BackoffSeconds,
	})
	consumer.SetAIResolver(aiFactory, aiProfileSvc)
	consumer.SetAIRecorder(aiObserver)
	consumer.SetRAGIndexProducer(deps.producer)
	if deps.ragStore != nil {
		consumer.SetRAGIndexer(func(ctx context.Context, task *model.VideoTask) error {
			profile, err := aiProfileSvc.GetDefaultAIProfile(task.UserID)
			if err != nil {
				return err
			}
			embeddingClient, err := aiFactory.NewEmbeddingClient(*profile)
			if err != nil {
				return err
			}
			_, err = ragIndexSvc.BuildTaskIndex(ctx, task.UserID, task.ID, embeddingClient, *profile)
			return err
		})
	}

	return &serverApplication{
		handlers: serverHandlers{
			user:           handler.NewUserHandler(userSvc),
			profiles:       handler.NewAIProfileHandler(aiProfileSvc),
			rag:            handler.NewRAGHandler(ragIndexSvc, aiProfileSvc, aiFactory),
			chat:           handler.NewChatHandler(chatSvc, aiProfileSvc, aiFactory),
			media:          handler.NewMediaHandler(mediaSvc),
			uploadSessions: handler.NewUploadSessionHandler(uploadSessionSvc),
		},
		rateLimiter: rateLimiter,
		consumer:    consumer,
		retryScheduler: mq.NewRetryScheduler(deps.repos, deps.producer, mq.RetrySchedulerConfig{
			BatchSize: deps.cfg.TaskRetry.BatchSize,
			Interval:  time.Duration(deps.cfg.TaskRetry.ScanIntervalSeconds) * time.Second,
		}),
		taskCleanup:          taskCleanup,
		taskCleanupScheduler: taskCleanupScheduler,
		kafka:                deps.cfg.Kafka,
	}, nil
}

func (a *serverApplication) Start(ctx context.Context) {
	a.consumer.StartAnalyzeConsumer(ctx, a.kafka.Brokers, a.kafka.AnalyzeTopic, a.kafka.ConsumerGroup)
	a.consumer.StartTranscribeConsumer(ctx, a.kafka.Brokers, a.kafka.TranscribeTopic, a.kafka.ConsumerGroup)
	a.consumer.StartDownloadConsumer(ctx, a.kafka.Brokers, a.kafka.DownloadTopic, a.kafka.ConsumerGroup)
	a.consumer.StartRAGIndexConsumer(ctx, a.kafka.Brokers, a.kafka.RAGIndexTopic, a.kafka.ConsumerGroup)
	a.retryScheduler.Start(ctx)
	a.taskCleanupScheduler.Start(ctx)
}

func (a *serverApplication) Wait() {
	a.consumer.Wait()
	a.retryScheduler.Wait()
	a.taskCleanupScheduler.Wait()
}
