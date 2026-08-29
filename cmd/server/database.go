package main

import (
	"context"
	"errors"
	"fmt"

	"vid-lens/internal/config"
	appdb "vid-lens/internal/database"
	"vid-lens/internal/model"
)

func openServerDatabase(ctx context.Context, cfg *config.Config) (*appdb.Connection, error) {
	if cfg == nil {
		return nil, errors.New("open server database: config is required")
	}
	connection, err := appdb.OpenPostgres(ctx, cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("open server database: %w", err)
	}
	if err := model.Migrate(connection.GORM); err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("migrate postgres database: %w", err)
	}
	return connection, nil
}
