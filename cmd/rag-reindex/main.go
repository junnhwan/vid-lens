package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/config"
	appdb "vid-lens/internal/database"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/secret"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
	"vid-lens/internal/vector"
)

type options struct {
	configPath      string
	userID          int64
	taskID          int64
	embeddingModel  string
	pageSize        int
	maxRetries      int
	retryBaseDelay  time.Duration
	checkpointPath  string
	all             bool
	resetCheckpoint bool
	execute         bool
	timeout         time.Duration
}

func main() {
	opts, err := parseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "rag reindex flags: %v\n", err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if opts.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.timeout)
		defer cancel()
	}
	result, err := run(ctx, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rag reindex failed: %v\n", err)
		os.Exit(1)
	}
	mode := "dry-run"
	if opts.execute {
		mode = "execute"
	}
	fmt.Printf("rag reindex %s complete: candidates=%d processed=%d last_chunk_id=%d\n", mode, result.Candidates, result.Processed, result.LastChunkID)
}

func parseFlags(args []string) (options, error) {
	flags := flag.NewFlagSet("rag-reindex", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var opts options
	flags.StringVar(&opts.configPath, "config", "config.yaml", "config file path")
	flags.Int64Var(&opts.userID, "user-id", 0, "optional user scope")
	flags.Int64Var(&opts.taskID, "task-id", 0, "optional task scope")
	flags.StringVar(&opts.embeddingModel, "model", "", "optional embedding model scope")
	flags.IntVar(&opts.pageSize, "page-size", 100, "PostgreSQL source page size (1-1000)")
	flags.IntVar(&opts.maxRetries, "max-retries", 2, "embedding retries after the first attempt")
	flags.DurationVar(&opts.retryBaseDelay, "retry-base-delay", 500*time.Millisecond, "base delay for embedding retries")
	flags.StringVar(&opts.checkpointPath, "checkpoint", ".logs/rag-reindex-pgvector.json", "resume checkpoint path")
	flags.BoolVar(&opts.all, "all", false, "explicitly allow execute mode without user/task/model filters")
	flags.BoolVar(&opts.resetCheckpoint, "reset-checkpoint", false, "ignore and replace an existing checkpoint")
	flags.BoolVar(&opts.execute, "execute", false, "perform paid embedding calls and pgvector writes; default is dry-run")
	flags.DurationVar(&opts.timeout, "timeout", 0, "optional overall timeout; zero means no deadline")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	opts.embeddingModel = strings.TrimSpace(opts.embeddingModel)
	opts.checkpointPath = strings.TrimSpace(opts.checkpointPath)
	if opts.userID < 0 || opts.taskID < 0 {
		return options{}, errors.New("user-id and task-id cannot be negative")
	}
	if opts.pageSize <= 0 || opts.pageSize > 1000 {
		return options{}, errors.New("page-size must be between 1 and 1000")
	}
	if opts.maxRetries < 0 {
		return options{}, errors.New("max-retries cannot be negative")
	}
	if opts.retryBaseDelay < 0 || opts.timeout < 0 {
		return options{}, errors.New("durations cannot be negative")
	}
	if opts.execute && opts.checkpointPath == "" {
		return options{}, errors.New("checkpoint path is required in execute mode")
	}
	if opts.execute && opts.userID == 0 && opts.taskID == 0 && opts.embeddingModel == "" && !opts.all {
		return options{}, errors.New("execute mode requires user-id, task-id, model, or an explicit --all")
	}
	return opts, nil
}

func validateReindexConfig(cfg *config.Config) error {
	return errors.Join(cfg.ValidatePostgres(), cfg.ValidatePGVectorDestination())
}

func run(ctx context.Context, opts options) (service.RAGReindexResult, error) {
	cfg, err := config.Load(opts.configPath)
	if err != nil {
		return service.RAGReindexResult{}, err
	}
	if err := validateReindexConfig(cfg); err != nil {
		return service.RAGReindexResult{}, err
	}
	connection, err := appdb.OpenPostgres(ctx, cfg.Database)
	if err != nil {
		return service.RAGReindexResult{}, fmt.Errorf("connect PostgreSQL: %w", err)
	}
	defer connection.Close()
	repos := repository.NewRepositories(connection.GORM)

	reindexOpts := service.RAGReindexOptions{
		UserID:               opts.userID,
		TaskID:               opts.taskID,
		EmbeddingModel:       opts.embeddingModel,
		DestinationDimension: cfg.RAG.EmbeddingDim,
		PageSize:             opts.pageSize,
		DryRun:               !opts.execute,
		MaxRetries:           opts.maxRetries,
		RetryBaseDelay:       opts.retryBaseDelay,
	}
	if !opts.execute {
		return service.NewRAGReindexer(repos.VideoChunk, nil, nil, nil).Run(ctx, reindexOpts)
	}

	signature, err := scopeSignature(checkpointScope{
		UserID: opts.userID, TaskID: opts.taskID, EmbeddingModel: opts.embeddingModel,
		PostgresHost: cfg.Database.Host, PostgresPort: cfg.Database.Port, PostgresDB: cfg.Database.DBName,
		PostgresTable: cfg.RAG.VectorTable, EmbeddingDim: cfg.RAG.EmbeddingDim,
	})
	if err != nil {
		return service.RAGReindexResult{}, err
	}
	state := checkpoint{Version: checkpointVersion, Signature: signature}
	if !opts.resetCheckpoint {
		loaded, found, err := loadCheckpoint(opts.checkpointPath)
		if err != nil {
			return service.RAGReindexResult{}, err
		}
		if found {
			if loaded.Signature != signature {
				return service.RAGReindexResult{}, errors.New("checkpoint does not match the current filters or pgvector destination; use --reset-checkpoint after verifying the scope")
			}
			state = loaded
		}
	}
	reindexOpts.AfterChunkID = state.LastChunkID
	lifecycle := newCheckpointLifecycle(opts.checkpointPath, &state, time.Now)
	return lifecycle.execute(func(lifecycle *checkpointLifecycle) (service.RAGReindexResult, error) {
		codecSecret := cfg.Security.APIKeySecret
		if codecSecret == "" {
			codecSecret = cfg.JWT.Secret
		}
		codec, err := secret.NewCodecFromPassphrase(codecSecret)
		if err != nil {
			return service.RAGReindexResult{}, fmt.Errorf("initialize API key codec: %w", err)
		}
		profiles := service.NewAIProfileService(repos.AIProfile, codec, nil)
		factory := ai.NewFactory()

		if err := lifecycle.enterStage(checkpointFailureConnectPGVector); err != nil {
			return service.RAGReindexResult{}, err
		}
		// This command intentionally targets pgvector even when rag.store is kept
		// on Milvus for rollback. Reuse the shared application adapter so pgvector
		// connection fields cannot drift from the server and audit commands.
		pgConfig := vector.BackendConfigFromApplication(cfg).PGVector
		pgConfig.MaxOpenConns = 4
		pgConfig.MaxIdleConns = 2
		pgStore, err := vector.NewPGVectorStore(ctx, pgConfig)
		if err != nil {
			return service.RAGReindexResult{}, fmt.Errorf("connect pgvector destination: %w", err)
		}
		defer pgStore.Close()

		if err := lifecycle.enterStage(checkpointFailureRebuildVectors); err != nil {
			return service.RAGReindexResult{}, err
		}
		// Persist progress only after pgvector upsert succeeds. If this write
		// fails, resuming may repeat one chunk, which is safe because upsert is
		// idempotent for the stable vector identity.
		reindexOpts.OnChunkComplete = func(chunk model.VideoChunk, processed int64) error {
			return lifecycle.saveProgress(chunk.ID, processed)
		}
		return service.NewRAGReindexer(repos.VideoChunk, profiles, factory, pgStore).Run(ctx, reindexOpts)
	})
}
