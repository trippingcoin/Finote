package db

import (
	"context"
	"database/sql"
	"finote/config"
	"fmt"
	"time"
)

func OpenDB(cfg config.Config) (*sql.DB, error) {

	deadline := time.Now().Add(30 * time.Second)

	for {
		db, err := sql.Open("postgres", cfg.DB.DSN)
		if err != nil {
			return nil, fmt.Errorf("open db: %w", err)
			time.Sleep(2 * time.Second)
			continue
		}

		db.SetConnMaxIdleTime(cfg.DB.ConnMaxIdleTime)
		db.SetConnMaxLifetime(cfg.DB.ConnMaxLifetime)

		ctx, cancel := context.WithTimeout(context.Background(), deadline.Sub(time.Now()))
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			return nil, fmt.Errorf("ping db: %w", err)
		}

		if err == nil || time.Now().After(deadline) {
			return db, nil
		}

		fmt.Println("Failed to ping DB, retrying...")
		time.Sleep(2 * time.Second)
	}
}
