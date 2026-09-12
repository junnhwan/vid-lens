package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"vid-lens/internal/config"
	appdb "vid-lens/internal/database"
	"vid-lens/internal/vector"
)

func newConfiguredVectorStore(ctx context.Context, cfg *config.Config) (vector.Store, error) {
	return vector.NewStore(ctx, vector.BackendConfigFromApplication(cfg))
}

func openEvalDatabase(ctx context.Context, cfg *config.Config) (*appdb.Connection, error) {
	return appdb.OpenPostgres(ctx, cfg.Database)
}

func validateEvalConfig(cfg *config.Config) error {
	return errors.Join(cfg.ValidatePostgres(), cfg.ValidateRAG())
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "product-candidates" {
		if err := runProductCandidatesCommand(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "product candidate operation failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "product" {
		if err := runProductCommand(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "product eval failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	opts, err := parseEvalFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "rag eval flags: %v\n", err)
		os.Exit(2)
	}
	if err := run(context.Background(), opts); err != nil {
		fmt.Fprintf(os.Stderr, "rag eval failed: %v\n", err)
		os.Exit(1)
	}
}
