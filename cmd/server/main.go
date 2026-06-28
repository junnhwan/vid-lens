package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
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
	"vid-lens/internal/vector"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type aiProfileTesterAdapter struct {
	tester *ai.ProfileTester
}

func (a *aiProfileTesterAdapter) TestProfile(ctx context.Context, profile *service.DecryptedAIProfile) error {
	return a.tester.TestProfile(ctx, ai.Profile{
		LLMProvider:       profile.LLMProvider,
		LLMBaseURL:        profile.LLMBaseURL,
		LLMAPIKey:         profile.LLMAPIKey,
		LLMModel:          profile.LLMModel,
		ASRProvider:       profile.ASRProvider,
		ASRBaseURL:        profile.ASRBaseURL,
		ASRAPIKey:         profile.ASRAPIKey,
		ASRModel:          profile.ASRModel,
		EmbeddingProvider: profile.EmbeddingProvider,
		EmbeddingEndpoint: profile.EmbeddingEndpoint,
		EmbeddingAPIKey:   profile.EmbeddingAPIKey,
		EmbeddingModel:    profile.EmbeddingModel,
		EmbeddingDim:      profile.EmbeddingDim,
	})
}

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 数据库
	db, err := gorm.Open(mysql.Open(cfg.Database.DSN()), &gorm.Config{})
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}
	if err := model.Migrate(db); err != nil {
		log.Fatalf("迁移数据库失败: %v", err)
	}
	log.Println("✅ 数据库连接成功")

	// Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr(),
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	log.Println("✅ Redis 连接成功")

	// MinIO
	minioStorage, err := storage.NewMinIOStorage(
		cfg.MinIO.Endpoint, cfg.MinIO.AccessKey, cfg.MinIO.SecretKey,
		cfg.MinIO.Bucket, cfg.MinIO.UseSSL,
	)
	if err != nil {
		log.Fatalf("初始化 MinIO 失败: %v", err)
	}
	log.Println("✅ MinIO 连接成功")

	// AI
	var aiStrategy ai.Strategy
	switch strings.ToLower(cfg.AI.Provider) {
	case "", "siliconflow":
		aiStrategy = ai.NewSiliconFlowStrategy(
			cfg.AI.SiliconFlowAPIKey, cfg.AI.SiliconFlowBaseURL,
			cfg.AI.ASRModel, cfg.AI.LLMModel,
		)
	case "mimo":
		aiStrategy = ai.NewMimoStrategy(
			cfg.AI.MimoAPIKey, cfg.AI.MimoBaseURL,
			cfg.AI.ASRModel, cfg.AI.LLMModel,
		)
	default:
		log.Fatalf("不支持的 AI provider: %s", cfg.AI.Provider)
	}

	// Kafka
	if cfg.Kafka.DownloadTopic == "" {
		cfg.Kafka.DownloadTopic = "video-download"
	}
	if cfg.Kafka.RAGIndexTopic == "" {
		cfg.Kafka.RAGIndexTopic = "video-rag-index"
	}
	mq.CreateTopics(cfg.Kafka.Brokers, []string{
		cfg.Kafka.AnalyzeTopic, cfg.Kafka.TranscribeTopic, cfg.Kafka.DownloadTopic, cfg.Kafka.RAGIndexTopic,
	})
	producer := mq.NewProducer(cfg.Kafka.Brokers, cfg.Kafka.AnalyzeTopic, cfg.Kafka.TranscribeTopic, cfg.Kafka.DownloadTopic, cfg.Kafka.RAGIndexTopic)
	defer producer.Close()
	log.Println("✅ Kafka 生产者就绪")

	repos := repository.NewRepositories(db)

	// Service & Handler
	apiKeySecret := cfg.Security.APIKeySecret
	if apiKeySecret == "" {
		apiKeySecret = cfg.JWT.Secret
		log.Println("⚠️ security.api_key_secret 未配置，临时复用 jwt.secret；公开部署请设置 VIDLENS_API_KEY_SECRET")
	}
	secretCodec, err := secret.NewCodecFromPassphrase(apiKeySecret)
	if err != nil {
		log.Fatalf("初始化 API Key 加密器失败: %v", err)
	}
	aiFactory := ai.NewFactory()
	userSvc := service.NewUserService(repos.User, cfg.JWT)
	aiProfileSvc := service.NewAIProfileService(repos.AIProfile, secretCodec, &aiProfileTesterAdapter{tester: ai.NewProfileTester(aiFactory)})
	var ragStore service.RAGVectorStore
	var ragRetriever service.RAGRetriever
	if cfg.RAG.Enabled {
		milvusCtx, cancelMilvus := context.WithTimeout(context.Background(), 5*time.Second)
		milvusStore, err := vector.NewMilvusStore(milvusCtx, vector.MilvusConfig{
			Address:    cfg.Milvus.Address,
			Username:   cfg.Milvus.Username,
			Password:   cfg.Milvus.Password,
			Token:      cfg.Milvus.Token,
			Database:   cfg.Milvus.Database,
			Collection: cfg.RAG.Collection,
			Dim:        cfg.RAG.EmbeddingDim,
		})
		cancelMilvus()
		if err != nil {
			log.Printf("⚠️ Milvus 连接失败，RAG 索引和视频问答暂不可用: %v", err)
		} else {
			defer func() {
				if err := milvusStore.Close(); err != nil {
					log.Printf("关闭 Milvus 连接失败: %v", err)
				}
			}()
			ragStore = milvusStore
			ragRetriever = milvusStore
			log.Println("✅ Milvus 向量库连接成功")
		}
	} else {
		log.Println("ℹ️ RAG 未启用，视频问答功能不可用")
	}
	ragIndexSvc := service.NewRAGIndexService(repos, ragStore, service.RAGIndexConfig{
		ChunkSize:      cfg.RAG.ChunkSize,
		ChunkOverlap:   cfg.RAG.ChunkOverlap,
		EmbeddingDim:   cfg.RAG.EmbeddingDim,
		CollectionName: cfg.RAG.Collection,
	})
	aiObserver := service.NewAIObserver(repos)
	ragIndexSvc.SetAIRecorder(aiObserver)
	chatSvc := service.NewChatService(repos, ragRetriever, service.ChatConfig{
		TopK:        cfg.RAG.TopK,
		CandidateK:  cfg.RAG.CandidateK,
		MinScore:    cfg.RAG.MinScore,
		RecentTurns: cfg.RAG.RecentTurns,
	})
	chatSvc.SetAIRecorder(aiObserver)
	chatSvc.SetMemoryStore(service.NewRedisChatMemoryStore(rdb))
	mediaSvc := service.NewMediaService(repos, minioStorage, producer, rdb, cfg.Upload, cfg.Tools)
	if cleaner, ok := ragStore.(interface {
		DeleteTaskChunks(ctx context.Context, userID, taskID int64, embeddingModel string) error
	}); ok {
		mediaSvc.SetTaskVectorCleaner(cleaner)
	}
	userHandler := handler.NewUserHandler(userSvc)
	aiProfileHandler := handler.NewAIProfileHandler(aiProfileSvc)
	ragHandler := handler.NewRAGHandler(ragIndexSvc, aiProfileSvc, aiFactory)
	chatHandler := handler.NewChatHandler(chatSvc, aiProfileSvc, aiFactory)
	mediaHandler := handler.NewMediaHandler(mediaSvc)
	rateLimiter := middleware.NewRateLimiter(rdb, cfg.RateLimit.Capacity, cfg.RateLimit.Rate)
	// 高成本 AI 接口按路由单独配更严格的限额（覆盖全局默认）
	for path, route := range cfg.RateLimit.Routes {
		rateLimiter.SetRouteLimit(path, route.Capacity, route.Rate)
	}

	consumer := mq.NewConsumer(repos, minioStorage, aiStrategy, rdb, cfg.Tools.FFmpegPath)
	consumer.SetDownloadTools(cfg.Tools.YtDlpPath, cfg.Tools.FFmpegPath, cfg.Tools.CookiesPath, cfg.Tools.ProxyURL)
	consumer.SetRetryPolicy(mq.TaskRetryPolicy{
		MaxRetries:     cfg.TaskRetry.MaxRetries,
		BackoffSeconds: cfg.TaskRetry.BackoffSeconds,
	})
	consumer.SetAIResolver(aiFactory, aiProfileSvc)
	consumer.SetAIRecorder(aiObserver)
	consumer.SetRAGIndexProducer(producer)
	if ragStore != nil {
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
	consumer.StartAnalyzeConsumer(cfg.Kafka.Brokers, cfg.Kafka.AnalyzeTopic, cfg.Kafka.ConsumerGroup)
	consumer.StartTranscribeConsumer(cfg.Kafka.Brokers, cfg.Kafka.TranscribeTopic, cfg.Kafka.ConsumerGroup)
	consumer.StartDownloadConsumer(cfg.Kafka.Brokers, cfg.Kafka.DownloadTopic, cfg.Kafka.ConsumerGroup)
	consumer.StartRAGIndexConsumer(cfg.Kafka.Brokers, cfg.Kafka.RAGIndexTopic, cfg.Kafka.ConsumerGroup)
	retryScheduler := mq.NewRetryScheduler(repos, producer, mq.RetrySchedulerConfig{
		BatchSize: cfg.TaskRetry.BatchSize,
		Interval:  time.Duration(cfg.TaskRetry.ScanIntervalSeconds) * time.Second,
	})
	retryScheduler.Start(context.Background())

	// HTTP
	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.Default()
	r.Use(middleware.CORS())

	api := r.Group("/api/v1")
	{
		api.POST("/user/register", userHandler.Register)
		api.POST("/user/login", userHandler.Login)

		auth := api.Group("")
		auth.Use(middleware.JWTAuth(cfg.JWT.Secret))
		{
			auth.GET("/user/profile", userHandler.GetProfile)
			aiProfiles := auth.Group("/ai/profiles")
			{
				aiProfiles.GET("", aiProfileHandler.List)
				aiProfiles.POST("", aiProfileHandler.Create)
				aiProfiles.PUT("/:id", aiProfileHandler.Update)
				aiProfiles.DELETE("/:id", aiProfileHandler.Delete)
				aiProfiles.POST("/test", aiProfileHandler.Test)
			}
			chat := auth.Group("/chat")
			{
				chat.POST("/sessions", chatHandler.CreateSession)
				chat.GET("/sessions", chatHandler.ListSessions)
				chat.GET("/sessions/:session_id/messages", chatHandler.ListMessages)
				chat.POST("/sessions/:session_id/messages", middleware.RateLimit(rateLimiter), chatHandler.Ask)
				chat.POST("/sessions/:session_id/messages/agent", middleware.RateLimit(rateLimiter), chatHandler.AskAgent)
				chat.POST("/sessions/:session_id/messages/stream", middleware.RateLimit(rateLimiter), chatHandler.AskStream)
			}
			media := auth.Group("/media")
			{
				media.POST("/upload", mediaHandler.UploadFile)
				media.POST("/upload-url", mediaHandler.UploadByURL)
				media.POST("/upload-chunk", mediaHandler.UploadChunk)
				media.GET("/check-upload", mediaHandler.CheckUpload)
				media.POST("/merge-chunks", mediaHandler.MergeChunks)
				media.GET("/list", mediaHandler.ListTasks)
				media.GET("/task/:id", mediaHandler.GetTaskDetail)
				media.DELETE("/task/:id", mediaHandler.DeleteTask)
				media.POST("/analyze/:id", middleware.RateLimit(rateLimiter), mediaHandler.RequestAnalysis)
				media.POST("/transcribe/:id", middleware.RateLimit(rateLimiter), mediaHandler.RequestTranscribe)
				media.GET("/task/:id/rag-index", ragHandler.GetTaskIndexStatus)
				media.POST("/task/:id/rag-index", middleware.RateLimit(rateLimiter), ragHandler.BuildTaskIndex)
				media.GET("/download-audio/:id", mediaHandler.DownloadAudio)
			}
		}
	}

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok", "service": "VidLens"})
	})

	// 前端静态文件
	staticDir := filepath.Join(".", "web", "dist")
	if info, err := os.Stat(staticDir); err == nil && info.IsDir() {
		r.Static("/assets", filepath.Join(staticDir, "assets"))
		r.NoRoute(func(c *gin.Context) { c.File(filepath.Join(staticDir, "index.html")) })
		log.Println("✅ 前端静态资源已加载")
	}

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("🚀 VidLens 服务启动在 http://localhost%s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
