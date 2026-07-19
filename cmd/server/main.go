package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"vid-lens/internal/ai"
	"vid-lens/internal/config"
	"vid-lens/internal/mq"
	"vid-lens/internal/observability"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
	"vid-lens/internal/storage"
	"vid-lens/internal/vector"
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

func loadServerConfig(path string) (*config.Config, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	if err := cfg.ValidateServer(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func runtimeServerHandlers(app *serverApplication) serverHandlers {
	return serverHandlers{
		user:           app.handlers.user,
		profiles:       app.handlers.profiles,
		rag:            app.handlers.rag,
		chat:           app.handlers.chat,
		media:          app.handlers.media,
		knowledgeBases: app.handlers.knowledgeBases,
	}
}

func main() {
	opts, err := parseServerOptions(os.Args[1:])
	if err != nil {
		log.Fatalf("解析启动参数失败: %v", err)
	}

	runtimeCtx, stopRuntime := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopRuntime()

	slog.SetDefault(observability.NewJSONLogger(os.Stdout, slog.LevelInfo))
	registry := prometheus.NewRegistry()
	metrics, err := observability.NewMetrics(registry)
	if err != nil {
		log.Fatalf("初始化 Prometheus 指标失败: %v", err)
	}
	observability.SetDefaultMetrics(metrics)
	metricsDone := make(chan error, 1)
	go func() { metricsDone <- serveMetrics(runtimeCtx, metrics.Handler()) }()

	cfg, err := loadServerConfig(opts.configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// PostgreSQL 同时承载业务关系数据和 pgvector 向量数据。
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 5*time.Second)
	dbConnection, err := openServerDatabase(startupCtx, cfg)
	cancelStartup()
	if err != nil {
		log.Fatalf("初始化 PostgreSQL 失败: %v", err)
	}
	defer func() {
		if err := dbConnection.Close(); err != nil {
			log.Printf("关闭 PostgreSQL 连接池失败: %v", err)
		}
	}()
	log.Println("✅ PostgreSQL 数据库连接成功")
	repos := repository.NewRepositories(dbConnection.GORM)

	// Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr(),
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	defer func() {
		if err := rdb.Close(); err != nil {
			log.Printf("关闭 Redis 连接失败: %v", err)
		}
	}()
	startupCtx, cancelStartup = context.WithTimeout(context.Background(), 5*time.Second)
	err = rdb.Ping(startupCtx).Err()
	cancelStartup()
	if err != nil {
		log.Fatalf("Redis 健康检查失败: %v", err)
	}
	log.Println("✅ Redis 连接成功")
	governance, err := newAIGovernanceRuntime(cfg.AIGovernance, rdb, repos)
	if err != nil {
		log.Fatalf("初始化 AI 调用治理失败: %v", err)
	}
	providerAdmission := governance.Admission
	startQuotaReconciler(runtimeCtx, governance.Reconciler, quotaReconcileInterval)

	// MinIO
	startupCtx, cancelStartup = context.WithTimeout(context.Background(), 5*time.Second)
	minioStorage, err := storage.NewMinIOStorageWithContext(startupCtx,
		cfg.MinIO.Endpoint, cfg.MinIO.AccessKey, cfg.MinIO.SecretKey,
		cfg.MinIO.Bucket, cfg.MinIO.UseSSL,
	)
	cancelStartup()
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
	aiStrategy = ai.AdmitStrategy(aiStrategy, providerAdmission, strings.ToLower(cfg.AI.Provider), cfg.AI.ASRModel, cfg.AI.LLMModel)

	// Kafka
	if err := mq.CreateTopics(cfg.Kafka.Brokers, []string{
		cfg.Kafka.AnalyzeTopic, cfg.Kafka.TranscribeTopic, cfg.Kafka.DownloadTopic, cfg.Kafka.RAGIndexTopic,
	}); err != nil {
		log.Fatalf("初始化 Kafka topics 失败: %v", err)
	}
	producer := mq.NewProducer(cfg.Kafka.Brokers, cfg.Kafka.AnalyzeTopic, cfg.Kafka.TranscribeTopic, cfg.Kafka.DownloadTopic, cfg.Kafka.RAGIndexTopic)
	defer producer.Close()
	log.Println("✅ Kafka 生产者就绪")

	var ragStore service.RAGVectorStore
	var ragRetriever service.RAGRetriever
	if cfg.RAG.Enabled {
		vectorCtx, cancelVector := context.WithTimeout(context.Background(), 5*time.Second)
		vectorStore, storeErr := vector.NewStore(vectorCtx, vector.BackendConfigFromApplication(cfg))
		cancelVector()
		if storeErr != nil {
			log.Printf("⚠️ vector backend 连接失败，RAG 索引和视频问答暂不可用: %v", storeErr)
		} else {
			defer func() {
				if err := vectorStore.Close(); err != nil {
					log.Printf("关闭 vector backend 连接失败: %v", err)
				}
			}()
			ragStore = vectorStore
			ragRetriever = vectorStore
			log.Printf("✅ vector backend 就绪: %s", vector.NormalizeBackendName(cfg.RAG.Store))
		}
	} else {
		log.Println("ℹ️ RAG 未启用，视频问答功能不可用")
	}

	app, err := wireServerApplication(serverDependencies{
		cfg:               cfg,
		repos:             repos,
		rdb:               rdb,
		minioStorage:      minioStorage,
		producer:          producer,
		providerAdmission: providerAdmission,
		ragStore:          ragStore,
		ragRetriever:      ragRetriever,
	}, aiStrategy)
	if err != nil {
		log.Fatalf("初始化应用组件失败: %v", err)
	}
	app.Start(runtimeCtx)

	// HTTP route and static frontend wiring lives in router.go.
	readinessChecks := []dependencyCheck{
		{Name: "database", Required: true, Check: func(ctx context.Context) error {
			return dbConnection.SQL.PingContext(ctx)
		}},
		{Name: "redis", Required: true, Check: func(ctx context.Context) error {
			return rdb.Ping(ctx).Err()
		}},
		{Name: "minio", Required: true, Check: minioStorage.HealthCheck},
		{Name: "kafka", Required: true, Check: func(ctx context.Context) error {
			return mq.PingBroker(ctx, cfg.Kafka.Brokers)
		}},
	}
	if cfg.RAG.Enabled {
		readinessChecks = append(readinessChecks, dependencyCheck{
			Name: "vector",
			// RAG is an explicitly enabled optional capability during the
			// current Milvus-to-pgvector migration period.
			Required: false,
			Check: func(ctx context.Context) error {
				if ragStore == nil {
					return fmt.Errorf("vector store unavailable")
				}
				healthChecker, ok := ragStore.(interface {
					HealthCheck(context.Context) error
				})
				if !ok {
					return fmt.Errorf("vector store health check unavailable")
				}
				return healthChecker.HealthCheck(ctx)
			},
		})
	}

	r := newServerRouter(*cfg, runtimeServerHandlers(app), app.rateLimiter, readinessChecks)

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
	server := &http.Server{Addr: addr, Handler: r}
	log.Printf("🚀 VidLens 服务启动在 http://localhost%s", addr)
	if err := serveHTTP(runtimeCtx, server, listener, httpShutdownTimeout); err != nil {
		log.Fatalf("服务运行失败: %v", err)
	}
	app.Wait()
	if err := <-metricsDone; err != nil {
		log.Printf("metrics server shutdown failed: %v", err)
	}
	log.Println("✅ VidLens 服务已优雅停止")
}
