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
		Kafka:     config.KafkaConfig{Brokers: []string{"127.0.0.1:9092"}, AnalyzeTopic: "analyze", TranscribeTopic: "transcribe", DownloadTopic: "download", RAGIndexTopic: "rag", ConsumerGroup: "test"},
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

func TestProductionRetrievalConfigUsesOriginalQueryWithoutExpansionOrRerank(t *testing.T) {
	cfg := productionRetrievalConfig(config.RAGConfig{TopK: 5, CandidateK: 17, MinScore: 0.25})
	if cfg.QueryMode != service.QueryModeOriginal || cfg.RewriteQueries != 1 {
		t.Fatalf("query config = mode:%q queries:%d, want original single query", cfg.QueryMode, cfg.RewriteQueries)
	}
	if !cfg.EnableVector || !cfg.EnableBM25 || cfg.RRFK != 60 {
		t.Fatalf("hybrid config = %+v", cfg)
	}
	if cfg.TopK != 5 || cfg.CandidateK != 17 {
		t.Fatalf("retrieval sizes = topK:%d candidateK:%d", cfg.TopK, cfg.CandidateK)
	}
	if cfg.NeighborRadius != 0 || cfg.RerankerMode != service.RerankerModeNone {
		t.Fatalf("post retrieval config = neighbor:%d reranker:%q", cfg.NeighborRadius, cfg.RerankerMode)
	}
}
