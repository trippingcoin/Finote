package db

import (
	"context"
	"database/sql"
	"finote/config"
	"fmt"

	_ "github.com/lib/pq"
)

func OpenDB(cfg config.Config) (*sql.DB, error) {
	database, err := sql.Open("postgres", cfg.DB.DSN)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	database.SetMaxOpenConns(cfg.DB.MaxOpenConns)
	database.SetMaxIdleConns(cfg.DB.MinIdleConns)
	database.SetConnMaxIdleTime(cfg.DB.ConnMaxIdleTime)
	database.SetConnMaxLifetime(cfg.DB.ConnMaxLifetime)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return database, nil
}
