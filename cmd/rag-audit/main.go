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

	"vid-lens/internal/config"
	appdb "vid-lens/internal/database"
	"vid-lens/internal/ragtool"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
	"vid-lens/internal/vector"
)

type options struct {
	configPath     string
	userID         int64
	taskID         int64
	embeddingModel string
	all            bool
	timeout        time.Duration
}

func main() {
	opts, err := parseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "rag audit flags: %v\n", err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if opts.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.timeout)
		defer cancel()
	}
	summary, err := run(ctx, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rag audit failed: %v\n", err)
		os.Exit(1)
	}
	writeAuditSummary(os.Stdout, summary)
	if !summary.Consistent() {
		fmt.Fprintln(os.Stderr, "rag audit found projection drift; review the report, then run cmd/rag-reindex with an explicit scope")
		os.Exit(1)
	}
}

func writeAuditSummary(w io.Writer, summary ragtool.RAGProjectionAuditSummary) {
	fmt.Fprintf(w, "rag audit complete: backend=%s scopes=%d source=%d target=%d issues=%d\n",
		vector.NormalizeBackendName(summary.Backend), len(summary.Scopes), summary.SourceCount, summary.TargetCount, summary.IssueCount())
	for _, report := range summary.Scopes {
		fmt.Fprintf(w, "scope: user=%d task=%d model=%q source=%d target=%d issues=%d\n",
			report.Scope.UserID, report.Scope.TaskID, report.Scope.EmbeddingModel,
			report.SourceCount, report.TargetCount, len(report.Issues))
		for _, message := range report.Messages() {
			fmt.Fprintf(w, "- %s\n", message)
		}
	}
}

func parseFlags(args []string) (options, error) {
	flags := flag.NewFlagSet("rag-audit", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var opts options
	flags.StringVar(&opts.configPath, "config", "config.yaml", "config file path")
	flags.Int64Var(&opts.userID, "user-id", 0, "PostgreSQL user scope (required)")
	flags.Int64Var(&opts.taskID, "task-id", 0, "PostgreSQL task scope (required)")
	flags.StringVar(&opts.embeddingModel, "model", "", "embedding model scope (required unless --all)")
	flags.BoolVar(&opts.all, "all", false, "audit every PostgreSQL and pgvector task/model scope")
	flags.DurationVar(&opts.timeout, "timeout", 0, "optional overall timeout; zero means no deadline")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	opts.configPath = strings.TrimSpace(opts.configPath)
	opts.embeddingModel = strings.TrimSpace(opts.embeddingModel)
	if opts.configPath == "" {
		return options{}, errors.New("config path is required")
	}
	if opts.all && (opts.userID != 0 || opts.taskID != 0 || opts.embeddingModel != "") {
		return options{}, errors.New("--all cannot be combined with --user-id, --task-id, or --model")
	}
	if !opts.all {
		if opts.userID <= 0 {
			return options{}, errors.New("user-id must be positive")
		}
		if opts.taskID <= 0 {
			return options{}, errors.New("task-id must be positive")
		}
		if opts.embeddingModel == "" {
			return options{}, errors.New("model is required")
		}
	}
	if opts.timeout < 0 {
		return options{}, errors.New("timeout cannot be negative")
	}
	return opts, nil
}

func validateAuditConfig(cfg *config.Config) error {
	return errors.Join(cfg.ValidatePostgres(), cfg.ValidateVectorBackend())
}

type ragAuditSource interface {
	ListEvidenceManifest(userID, taskID int64, model string) ([]repository.ChunkEvidenceManifestEntry, error)
}

type allRAGAuditSource interface {
	ListAllEvidenceManifest(context.Context) ([]repository.ChunkEvidenceManifestEntry, error)
}

type ragAuditTarget interface {
	ListTaskVectorManifest(context.Context, int64, int64, string) ([]service.RAGVectorManifestEntry, error)
}

type allRAGAuditTarget interface {
	ListAllVectorManifest(context.Context) ([]service.RAGVectorManifestEntry, error)
}

func run(ctx context.Context, opts options) (ragtool.RAGProjectionAuditSummary, error) {
	cfg, err := config.Load(opts.configPath)
	if err != nil {
		return ragtool.RAGProjectionAuditSummary{}, err
	}
	if err := validateAuditConfig(cfg); err != nil {
		return ragtool.RAGProjectionAuditSummary{}, err
	}
	connection, err := appdb.OpenPostgres(ctx, cfg.Database)
	if err != nil {
		return ragtool.RAGProjectionAuditSummary{}, fmt.Errorf("connect PostgreSQL: %w", err)
	}
	defer connection.Close()

	store, err := vector.NewStore(ctx, vector.BackendConfigFromApplication(cfg))
	if err != nil {
		return ragtool.RAGProjectionAuditSummary{}, fmt.Errorf("connect vector backend: %w", err)
	}
	defer store.Close()

	repo := repository.NewVideoChunkRepository(connection.GORM)
	return auditConfiguredProjection(ctx, opts, cfg.RAG.Store, repo, store)
}

// auditConfiguredProjection owns the command-level choice between one explicit
// scope and the migration-wide pgvector gate. Keeping database construction in
// run and manifest orchestration here makes the behavior testable without live
// infrastructure and prevents maintenance commands from reimplementing audit
// semantics.
func auditConfiguredProjection(ctx context.Context, opts options, backend string, source ragAuditSource, target ragAuditTarget) (ragtool.RAGProjectionAuditSummary, error) {
	backend = vector.NormalizeBackendName(backend)
	if opts.all {
		if backend != vector.DefaultBackend {
			return ragtool.RAGProjectionAuditSummary{}, fmt.Errorf("--all requires the pgvector backend, got %q", backend)
		}
		allSource, ok := source.(allRAGAuditSource)
		if !ok {
			return ragtool.RAGProjectionAuditSummary{}, errors.New("PostgreSQL source does not support an all-scope chunk manifest")
		}
		allTarget, ok := target.(allRAGAuditTarget)
		if !ok {
			return ragtool.RAGProjectionAuditSummary{}, errors.New("pgvector target does not support an all-scope vector manifest")
		}
		sourceManifest, err := allSource.ListAllEvidenceManifest(ctx)
		if err != nil {
			return ragtool.RAGProjectionAuditSummary{}, fmt.Errorf("list all PostgreSQL chunk manifests: %w", err)
		}
		targetManifest, err := allTarget.ListAllVectorManifest(ctx)
		if err != nil {
			return ragtool.RAGProjectionAuditSummary{}, fmt.Errorf("list all pgvector manifests: %w", err)
		}
		return ragtool.AuditAllRAGProjections(backend, ragSourceManifest(sourceManifest), targetManifest)
	}

	sourceManifest, err := source.ListEvidenceManifest(opts.userID, opts.taskID, opts.embeddingModel)
	if err != nil {
		return ragtool.RAGProjectionAuditSummary{}, fmt.Errorf("list PostgreSQL chunk manifest: %w", err)
	}
	targetManifest, err := target.ListTaskVectorManifest(ctx, opts.userID, opts.taskID, opts.embeddingModel)
	if err != nil {
		return ragtool.RAGProjectionAuditSummary{}, fmt.Errorf("list %s vector manifest: %w", backend, err)
	}
	report, err := ragtool.AuditRAGProjection(ragtool.RAGProjectionScope{
		UserID: opts.userID, TaskID: opts.taskID, EmbeddingModel: opts.embeddingModel, Backend: backend,
	}, ragSourceManifest(sourceManifest), targetManifest)
	if err != nil {
		return ragtool.RAGProjectionAuditSummary{}, err
	}
	return ragtool.RAGProjectionAuditSummary{
		Backend: backend, SourceCount: report.SourceCount, TargetCount: report.TargetCount,
		Scopes: []ragtool.RAGProjectionAuditReport{report},
	}, nil
}

func ragSourceManifest(entries []repository.ChunkEvidenceManifestEntry) []ragtool.RAGSourceManifestEntry {
	out := make([]ragtool.RAGSourceManifestEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, ragtool.RAGSourceManifestEntry{
			EvidenceID: entry.EvidenceID, UserID: entry.UserID, TaskID: entry.TaskID,
			ChunkID: entry.ChunkID, ChunkIndex: entry.ChunkIndex, ContentHash: entry.ContentHash,
			EmbeddingModel: entry.EmbeddingModel,
		})
	}
	return out
}
