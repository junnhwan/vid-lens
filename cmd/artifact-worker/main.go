// artifact-worker runs only artifact dispatch, recovery and generation. No HTTP/media workers.
package main

import (
	"context"
	"flag"
	"github.com/redis/go-redis/v9"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
	"vid-lens/internal/ai"
	"vid-lens/internal/config"
	"vid-lens/internal/database"
	"vid-lens/internal/model"
	"vid-lens/internal/mq"
	"vid-lens/internal/pkg/quota"
	"vid-lens/internal/pkg/secret"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
)

func main() {
	path := flag.String("config", "config.yaml", "configuration path")
	migrate := flag.Bool("migrate", false, "apply schema migrations before starting; normally the API server migrates first")
	flag.Parse()
	cfg, err := config.Load(*path)
	if err != nil {
		log.Fatal("load worker configuration failed")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := database.OpenPostgres(ctx, cfg.Database)
	if err != nil {
		log.Fatal("open worker database failed")
	}
	defer db.Close()
	if *migrate {
		if err = model.Migrate(db.GORM); err != nil {
			log.Fatal("migrate worker database failed")
		}
	}
	repos := repository.NewRepositories(db.GORM)
	rdb := redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr(), Password: cfg.Redis.Password, DB: cfg.Redis.DB})
	defer rdb.Close()
	key := cfg.Security.APIKeySecret
	if key == "" {
		key = cfg.JWT.Secret
	}
	codec, err := secret.NewCodecFromPassphrase(key)
	if err != nil {
		log.Fatal("initialize worker credential codec failed")
	}
	cache := quota.NewRedisUsageCache(rdb, 48*time.Hour)
	// Expensive generation fails closed when quota storage is unavailable.
	admission := &ai.QuotaAdmission{Limiter: quota.NewLimiterWithPolicies(rdb, quota.OperationPolicies{Default: quota.FailClosed}), Attempts: repos.RetryBudget, Usage: service.NewAIUsageGovernor(repos, cache, time.Local), User: ai.BucketRule{Capacity: 30, Rate: 0.5}, Operation: ai.BucketRule{Capacity: 30, Rate: 0.5}, Provider: ai.BucketRule{Capacity: 20, Rate: 0.33}, Model: ai.BucketRule{Capacity: 10, Rate: 0.16}}
	reconciler := quota.NewReconciler(repos.QuotaCompensation, cache, quota.ReconcilerConfig{})
	reconciled := reconciler.Start(ctx, 30*time.Second)
	profiles := service.NewAIProfileService(repos.AIProfile, codec, nil).WithAgentBudgetConfig(cfg.AgentBudget)
	svc := service.NewArtifactService(repos, profiles, ai.NewFactoryWithAdmission(admission))
	worker := mq.NewArtifactWorker(repos.Artifact, svc, cfg.MQ.Brokers)
	worker.Start(ctx)
	<-ctx.Done()
	worker.Wait()
	<-reconciled
}
