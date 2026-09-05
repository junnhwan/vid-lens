package main

import (
	"context"
	"fmt"
	"log"
	"strings"
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
	mq                   config.MQConfig
	memoryWriter         *service.AsyncMemoryWriter
	memoryCapture        *service.AsyncMemoryCapture
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

// productionRetrievalConfig is the eval-driven production retrieval configuration.
//
// docs/architecture/retrieval.md B段（评测驱动线上化）：rerank 默认值由 docs/eval/README.md dev 单变量消融结论驱动，
// 不再靠 cfg.RerankModel 是否非空手拍。当前实现约束与 audit trail：
//
//   - experiment_id: rerank-vs-none-dev（docs-private/eval/experiment-registry.yaml +
//     artifacts/eval/rag-results.md "Strict Single-Variable Ablation"）
//   - dev split 6 case, frozen evidence: baseline rrf_fusion(rerank=none) nDCG@5=0.731,
//     candidate rrf_rerank(rerank=deterministic) nDCG@5=0.833.
//   - observed effect +0.102, bootstrap 95% CI [0,+0.204], status=passed
//     (lower bound ≥ minimum_effect=0; guardrail answerability_f1 回归 0).
//   - 故默认 RerankerMode=deterministic / RerankerVersion=deterministic-v1.
//
// External-claim guardrail (keep public descriptions aligned with runtime behavior):
//   - deterministic rerank 非真实 model-rerank：strict eval 路径无
//     ModelRerankerFactory，rrf_rerank 用 DeterministicReranker 代理。线上 model-rerank
//     效果由 (B) 在线对比测，不由本节数字支撑。cfg.RerankModel 非空时仍升级到
//     model rerank（保留显式覆盖路径），但 lift 未由本实验证明。
//   - BM25 hybrid 非单变量：vector→+BM25 伴随 enable_bm25+candidate_k+rrf_k 三因子同变
//     （架构约束：hybrid 必须 RRF + 扩候选池），无法做成 strict 单变量对。BM25 在线上
//     保守关闭（EnableBM25=false），其收益只能从 legacy 50-case 报告引用，不进 strict
//     单变量证据链。
func productionRetrievalConfig(cfg config.RAGConfig) service.RAGRetrievalConfig {
	retrieval := service.DefaultRAGRetrievalConfig()
	retrieval.QueryMode = service.QueryModeOriginal
	retrieval.RewriteQueries = 1
	if cfg.RewriteQueries > 1 {
		retrieval.QueryMode = service.QueryModeRewrite
		retrieval.RewriteQueries = cfg.RewriteQueries
	}
	retrieval.TopK = cfg.TopK
	retrieval.CandidateK = cfg.CandidateK
	// BM25 hybrid 非单变量未评测（见上方 HONEST），保守关闭。
	retrieval.EnableBM25 = false
	retrieval.RRFK = 60
	retrieval.NeighborRadius = 0
	retrieval.MaxContextChars = 0
	retrieval.MinVectorScore = cfg.MinScore
	// docs/architecture/retrieval.md B段：rerank 默认 deterministic on（experiment rerank-vs-none-dev
	// dev 消融 +0.102 CI [0,+0.204]，deterministic 代理）。cfg.RerankModel 非空时
	// 升级到 model rerank（显式覆盖路径，lift 未由本实验证明）。
	retrieval.RerankerMode = service.RerankerModeDeterministic
	retrieval.RerankerVersion = "deterministic-v1"
	if strings.TrimSpace(cfg.RerankModel) != "" {
		retrieval.RerankerMode = service.RerankerModeModel
		retrieval.RerankerVersion = strings.TrimSpace(cfg.RerankModel)
	}
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
	knowledgeBaseSvc := service.NewKnowledgeBaseService(deps.repos)
	aiProfileSvc := service.NewAIProfileService(deps.repos.AIProfile, secretCodec, &aiProfileTesterAdapter{tester: ai.NewProfileTester(aiFactory)})
	if err := service.EnsureDemoAccount(deps.repos.User, deps.repos.AIProfile, secretCodec, deps.cfg.AI, deps.cfg.RAG); err != nil {
		log.Printf("⚠️ 演示账号初始化失败: %v", err)
	}
	ragIndexSvc := service.NewRAGIndexService(deps.repos, deps.ragStore, service.RAGIndexConfig{
		ChunkSize:    deps.cfg.RAG.ChunkSize,
		ChunkOverlap: deps.cfg.RAG.ChunkOverlap,
		EmbeddingDim: deps.cfg.RAG.EmbeddingDim,
	})
	aiObserver := service.NewAIObserver(deps.repos)
	ragIndexSvc.SetAIRecorder(aiObserver)

	retrievalCfg := productionRetrievalConfig(deps.cfg.RAG)
	modelRerankerFactory := func(profile ai.Profile) service.Reranker {
		client, err := aiFactory.NewRerankClient(profile)
		if err != nil {
			log.Printf("model reranker unavailable, falling back to vector order: %v", err)
			return service.NewModelReranker(nil)
		}
		return service.NewModelReranker(client)
	}
	chatConfig := service.ChatConfig{
		TopK:                 deps.cfg.RAG.TopK,
		CandidateK:           deps.cfg.RAG.CandidateK,
		MinScore:             deps.cfg.RAG.MinScore,
		RecentTurns:          deps.cfg.RAG.RecentTurns,
		Retrieval:            &retrievalCfg,
		ModelRerankerFactory: modelRerankerFactory,
	}
	evidenceLedgerSvc := service.NewEvidenceLedgerService(deps.repos)
	memoryAuthorizer := service.NewRepositoryMemoryAuthorizer(deps.repos)
	memoryGovernanceSvc := service.NewMemoryGovernanceService(deps.repos.Memory, memoryAuthorizer)
	memoryPolicySvc := service.NewMemoryPolicyService(deps.repos.Memory, deps.cfg.Memory.Enabled)
	var memoryWriter *service.AsyncMemoryWriter
	var memoryCapture *service.AsyncMemoryCapture
	var longTermMemory service.MemoryProvider
	if deps.cfg.Memory.Enabled && deps.repos.Memory != nil {
		var projector service.MemoryProjector
		var memoryEmbedder service.MemoryEmbedder
		if err := deps.repos.Memory.EnsureEmbeddingSchema(context.Background(), deps.cfg.RAG.EmbeddingDim); err != nil {
			log.Printf("agent memory embedding projection unavailable; relational memory remains enabled: %v", err)
		} else {
			memoryEmbedder = service.MemoryEmbedderFunc(func(ctx context.Context, userID int64, content string) (service.MemoryEmbedding, error) {
				profile, err := aiProfileSvc.GetDefaultAIProfile(userID)
				if err != nil {
					return service.MemoryEmbedding{}, err
				}
				client, err := aiFactory.NewEmbeddingClient(*profile)
				if err != nil {
					return service.MemoryEmbedding{}, err
				}
				vector, err := client.Embed(ctx, content)
				if err != nil {
					return service.MemoryEmbedding{}, err
				}
				return service.MemoryEmbedding{Model: profile.EmbeddingModel, Vector: vector}, nil
			})
			projector = service.NewRepositoryMemoryProjector(memoryEmbedder, deps.repos.Memory)
		}
		var memoryRetriever service.MemoryRetriever = service.NewRepositoryMemoryRetriever(deps.repos.Memory)
		if memoryEmbedder != nil {
			memoryRetriever = service.NewSemanticMemoryRetriever(deps.repos.Memory, memoryEmbedder)
		}
		provider := service.NewScopedMemoryProvider(memoryRetriever, memoryAuthorizer,
			service.AgentMemoryConfig{TopK: deps.cfg.Memory.TopK, MaxChars: deps.cfg.Memory.MaxChars, MaxTokens: deps.cfg.Memory.MaxTokens})
		longTermMemory = provider
		memoryWriter = service.NewAsyncMemoryWriter(deps.repos.Memory, memoryAuthorizer, projector, deps.cfg.Memory.QueueSize)
		memoryCapture = service.NewAsyncMemoryCapture(service.ExplicitPreferenceExtractor{}, memoryWriter, deps.cfg.Memory.QueueSize)
	}
	chatSvc := service.NewChatServiceWithDependencies(deps.repos, deps.ragRetriever, chatConfig, service.ChatDependencies{
		Memory: service.NewRedisChatMemoryStore(deps.rdb), LongTermMemory: longTermMemory, MemoryCapture: memoryCapture,
		MemoryPolicy:   memoryPolicySvc,
		EvidenceLedger: evidenceLedgerSvc, Recorder: aiObserver,
		IntentRouter: service.NewIntentRouter(service.NewRuleIntentClassifier()),
	})

	mediaSvc := service.NewMediaService(deps.repos, deps.minioStorage, deps.producer, deps.rdb, deps.cfg.Upload, deps.cfg.Tools)
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
	consumer.SetMQConfig(deps.cfg.MQ.Brokers, deps.cfg.MQ.Prefetch)
	asrBackoffs := make([]time.Duration, 0, len(deps.cfg.MQ.ASRRetryBackoffMS))
	for _, milliseconds := range deps.cfg.MQ.ASRRetryBackoffMS {
		if milliseconds > 0 {
			asrBackoffs = append(asrBackoffs, time.Duration(milliseconds)*time.Millisecond)
		}
	}
	consumer.SetASRProcessingConfig(deps.cfg.MQ.ASRConcurrency, deps.cfg.MQ.ASRMaxRetries, asrBackoffs)
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

	visualCfg := service.DefaultVisualIndexConfig()
	if cmd := strings.TrimSpace(deps.cfg.Tools.OCRPath); cmd != "" {
		visualCfg.OCRCommand = cmd
	}
	if lang := strings.TrimSpace(deps.cfg.Tools.OCRLang); lang != "" {
		visualCfg.OCRLang = lang
	}
	// Visual index stays enabled: Vision BYOK and/or local OCR decide what runs.
	visualIndexSvc := service.NewVisualIndexService(deps.repos, deps.minioStorage, deps.cfg.Tools.FFmpegPath, visualCfg)
	visionResolver := func(ctx context.Context, userID int64) (ai.VisionClient, error) {
		profile, err := aiProfileSvc.GetDefaultAIProfile(userID)
		if err != nil {
			return nil, err
		}
		if !ai.VisionConfigured(*profile) {
			return nil, fmt.Errorf("vision not configured on default AI profile")
		}
		return aiFactory.NewVisionClient(*profile)
	}
	visualIndexSvc.SetVisionResolver(visionResolver)
	consumer.SetVisualIndexer(func(ctx context.Context, task *model.VideoTask) (int, error) {
		return visualIndexSvc.BuildTaskVisualIndex(ctx, task)
	})

	videoAgentSvc := service.NewVideoAgentService(chatSvc)
	visualInvestigator := service.NewVisualInvestigator(deps.repos, deps.minioStorage, deps.cfg.Tools.FFmpegPath)
	visualInvestigator.SetVisionResolver(visionResolver)
	visualInvestigator.SetVisionModelResolver(func(ctx context.Context, userID int64) (string, error) {
		profile, err := aiProfileSvc.GetDefaultAIProfile(userID)
		if err != nil {
			return "", err
		}
		return profile.VisionModel, nil
	})
	videoAgentSvc.SetVisualInvestigator(visualInvestigator)
	videoAgentSvc.SetEvidenceInspectorVisualVerifier(visionResolver, deps.minioStorage.DownloadToTemp)
	conversationExecution := service.NewConversationExecution(chatSvc, videoAgentSvc, aiProfileSvc, aiFactory)
	chatHandler := handler.NewChatHandler(chatSvc, conversationExecution)
	chatHandler.SetEvidenceLedgerService(evidenceLedgerSvc)
	return &serverApplication{
		handlers: serverHandlers{
			user:           handler.NewUserHandler(userSvc),
			profiles:       handler.NewAIProfileHandler(aiProfileSvc),
			rag:            handler.NewRAGHandler(ragIndexSvc, aiProfileSvc, aiFactory),
			chat:           chatHandler,
			media:          handler.NewMediaHandler(mediaSvc),
			knowledgeBases: handler.NewKnowledgeBaseHandler(knowledgeBaseSvc),
			memory:         handler.NewMemoryHandler(memoryGovernanceSvc, memoryPolicySvc),
		},
		rateLimiter: rateLimiter,
		consumer:    consumer,
		retryScheduler: mq.NewRetryScheduler(deps.repos, deps.producer, mq.RetrySchedulerConfig{
			BatchSize: deps.cfg.TaskRetry.BatchSize,
			Interval:  time.Duration(deps.cfg.TaskRetry.ScanIntervalSeconds) * time.Second,
		}),
		taskCleanup:          taskCleanup,
		taskCleanupScheduler: taskCleanupScheduler,
		mq:                   deps.cfg.MQ,
		memoryWriter:         memoryWriter,
		memoryCapture:        memoryCapture,
	}, nil
}

func (a *serverApplication) Start(ctx context.Context) {
	a.consumer.StartAnalyzeConsumer(ctx, a.mq.Brokers, a.mq.AnalyzeQueue, a.mq.ConsumerGroup)
	a.consumer.StartTranscribeConsumer(ctx, a.mq.Brokers, a.mq.TranscribeQueue, a.mq.ConsumerGroup)
	a.consumer.StartDownloadConsumer(ctx, a.mq.Brokers, a.mq.DownloadQueue, a.mq.ConsumerGroup)
	a.consumer.StartRAGIndexConsumer(ctx, a.mq.Brokers, a.mq.RAGIndexQueue, a.mq.ConsumerGroup)
	a.retryScheduler.Start(ctx)
	a.taskCleanupScheduler.Start(ctx)
}

func (a *serverApplication) Wait() {
	a.consumer.Wait()
	a.retryScheduler.Wait()
	a.taskCleanupScheduler.Wait()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if a.memoryCapture != nil {
		_ = a.memoryCapture.Close(shutdownCtx)
	}
	if a.memoryWriter != nil {
		_ = a.memoryWriter.Close(shutdownCtx)
	}
}
