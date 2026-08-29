package main

import (
	"context"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"vid-lens/internal/ai"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
	"vid-lens/internal/mq"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
	"vid-lens/internal/storage"
)

func TestServerDependenciesValidateReportsMissingInfrastructure(t *testing.T) {
	base := serverDependencies{
		cfg:          &config.Config{},
		repos:        &repository.Repositories{},
		rdb:          redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"}),
		minioStorage: &storage.MinIOStorage{},
		producer:     &mq.Producer{},
	}
	cases := []struct {
		name string
		deps serverDependencies
		ai   ai.Strategy
		want string
	}{
		{name: "config", deps: serverDependencies{}, want: "config"},
		{name: "repositories", deps: func() serverDependencies { d := base; d.cfg = &config.Config{}; d.repos = nil; return d }(), want: "repositories"},
		{name: "redis", deps: func() serverDependencies { d := base; d.rdb = nil; return d }(), want: "redis"},
		{name: "minio", deps: func() serverDependencies { d := base; d.minioStorage = nil; return d }(), want: "minio"},
		{name: "producer", deps: func() serverDependencies { d := base; d.producer = nil; return d }(), want: "producer"},
		{name: "strategy", deps: base, want: "AI strategy"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.deps.validate(tc.ai)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validate() error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

type wiringTestStrategy struct{}

func (wiringTestStrategy) Transcribe(context.Context, string) (string, error) { return "", nil }
func (wiringTestStrategy) TranscribeChunks(context.Context, []string) (string, error) {
	return "", nil
}
func (wiringTestStrategy) Summarize(context.Context, string) (string, error) { return "", nil }

func TestWireServerApplicationIncludesDurableTaskCleanup(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repos := repository.NewRepositories(db)
	redisServer := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	cfg := &config.Config{
		Security:  config.SecurityConfig{APIKeySecret: "wiring-test-secret"},
		Upload:    config.UploadConfig{MaxFileSize: 1024, ChunkSize: 128},
		TaskRetry: config.TaskRetryConfig{ScanIntervalSeconds: 30, BatchSize: 20},
		Cleanup:   config.CleanupConfig{ScanIntervalSeconds: 30, BatchSize: 20, LeaseSeconds: 120, RetryBackoffSeconds: 60},
		RateLimit: config.RateLimitConfig{Capacity: 10, Rate: 10},
		MQ:        config.MQConfig{Brokers: []string{"127.0.0.1:5672"}, AnalyzeQueue: "analyze", TranscribeQueue: "transcribe", DownloadQueue: "download", RAGIndexQueue: "rag", ConsumerGroup: "test"},
	}
	app, err := wireServerApplication(serverDependencies{
		cfg:          cfg,
		repos:        repos,
		rdb:          rdb,
		minioStorage: &storage.MinIOStorage{},
		producer:     &mq.Producer{},
	}, wiringTestStrategy{})
	if err != nil {
		t.Fatalf("wireServerApplication() error = %v", err)
	}
	if app.handlers.knowledgeBases == nil {
		t.Fatal("knowledge base handler was not wired")
	}
	if app.taskCleanup == nil || app.taskCleanupScheduler == nil {
		t.Fatalf("cleanup wiring = service:%v scheduler:%v", app.taskCleanup, app.taskCleanupScheduler)
	}

	job := &model.TaskCleanupJob{TaskID: 101, UserID: 7, Status: model.TaskCleanupStatusPending}
	if err := repos.TaskCleanup.Create(job); err != nil {
		t.Fatal(err)
	}
	if err := app.taskCleanupScheduler.RunOnce(context.Background()); err != nil {
		t.Fatalf("wired cleanup scheduler RunOnce() error = %v", err)
	}
	stored, err := repos.TaskCleanup.FindByID(job.ID)
	if err != nil || stored == nil || stored.Status != model.TaskCleanupStatusCompleted {
		t.Fatalf("wired cleanup job = %+v, %v", stored, err)
	}
}

func TestProductionRetrievalConfigUsesOriginalQueryWithoutExpansion(t *testing.T) {
	cfg := productionRetrievalConfig(config.RAGConfig{TopK: 5, CandidateK: 17, MinScore: 0.25})
	if cfg.QueryMode != service.QueryModeOriginal || cfg.RewriteQueries != 1 {
		t.Fatalf("query config = mode:%q queries:%d, want original single query", cfg.QueryMode, cfg.RewriteQueries)
	}
	if !cfg.EnableVector || cfg.EnableBM25 || cfg.RRFK != 60 {
		t.Fatalf("hybrid config = %+v", cfg)
	}
	if cfg.TopK != 5 || cfg.CandidateK != 17 {
		t.Fatalf("retrieval sizes = topK:%d candidateK:%d", cfg.TopK, cfg.CandidateK)
	}
	if cfg.NeighborRadius != 0 {
		t.Fatalf("post retrieval config = neighbor:%d, want 0 (no expansion)", cfg.NeighborRadius)
	}
}

// TestProductionRetrievalConfigAppliesEvalConclusion 锁定 docs/architecture/retrieval.md B段：rerank 默认
// 值由 docs/eval/README.md dev 单变量消融结论驱动（experiment rerank-vs-none-dev, +0.102
// CI [0,+0.204], passed），不再靠 cfg.RerankModel 是否非空手拍。BM25 因非单变量
// 未评测保守关闭。任何回退到"rerank 默认 none / BM25 默认 on"的改动都会被此测试
// 抓住，防止线上化结论漂移。
func TestProductionRetrievalConfigAppliesEvalConclusion(t *testing.T) {
	cfg := productionRetrievalConfig(config.RAGConfig{TopK: 5, CandidateK: 17, MinScore: 0.25})
	if cfg.RerankerMode != service.RerankerModeDeterministic {
		t.Fatalf("rerank default = %q, want deterministic (eval rerank-vs-none-dev +0.102 CI[0,+0.204] passed)", cfg.RerankerMode)
	}
	if cfg.RerankerVersion != "deterministic-v1" {
		t.Fatalf("rerank version = %q, want deterministic-v1", cfg.RerankerVersion)
	}
	if cfg.EnableBM25 {
		t.Fatalf("BM25 = on, want off (BM25 hybrid 非单变量未评测, 保守关闭)")
	}
}

func TestProductionRetrievalConfigUsesVectorModelRerank(t *testing.T) {
	cfg := productionRetrievalConfig(config.RAGConfig{
		TopK: 5, CandidateK: 20, MinScore: 0.25, RewriteQueries: 3,
		RerankModel: "Qwen/Qwen3-Reranker-4B",
	})
	if cfg.QueryMode != service.QueryModeRewrite || cfg.RewriteQueries != 3 {
		t.Fatalf("rewrite config = %q/%d", cfg.QueryMode, cfg.RewriteQueries)
	}
	if !cfg.EnableVector || cfg.EnableBM25 {
		t.Fatalf("retriever config = vector:%v bm25:%v", cfg.EnableVector, cfg.EnableBM25)
	}
	if cfg.RerankerMode != service.RerankerModeModel || cfg.RerankerVersion != "Qwen/Qwen3-Reranker-4B" {
		t.Fatalf("reranker config = %q/%q", cfg.RerankerMode, cfg.RerankerVersion)
	}
}
