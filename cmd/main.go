package main

import (
	"context"
	"errors"
	"finote/config"
	"finote/internal/infrastucture/db"
	"finote/internal/migrations"
	"finote/internal/payment/httpapi"
	"finote/internal/payment/repo"
	"finote/internal/payment/service"
	"finote/internal/telegram"
	"finote/utils/env"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"
)

func main() {
	logger := slog.New(
		slog.NewJSONHandler(
			os.Stdout,
			&slog.HandlerOptions{Level: slog.LevelDebug},
		),
	)
	slog.SetDefault(logger)
	if err := run(); err != nil {
		logger.Error("application failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	var cfg config.Config
	if err := env.Load(&cfg); err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	database, err := db.OpenDB(cfg)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	migrateCtx, cancelMigrations := context.WithTimeout(ctx, 30*time.Second)
	err = migrations.Apply(migrateCtx, database)
	cancelMigrations()
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	var bot *telegram.Bot
	if cfg.Telegram.Token != "" {
		bot, err = configureBot(cfg, database)
		if err != nil {
			slog.Error("Telegram disabled by configuration; web panel remains available", "error", err)
		}
	}
	handler := httpapi.New(service.New(repo.New(database)))
	address := fmt.Sprintf(":%d", cfg.Port)
	slog.Info("starting payment API", "address", address)
	server := &http.Server{Addr: address, Handler: healthHandler(handler, database), ReadHeaderTimeout: 5 * time.Second}
	failures := make(chan error, 2)
	go func() { failures <- server.ListenAndServe() }()
	botDone := make(chan struct{})
	if bot != nil {
		go func() { defer close(botDone); telegram.RunIsolated(ctx, bot.Run) }()
	} else {
		close(botDone)
		slog.Info("Telegram is disabled")
	}
	var runErr error
	select {
	case <-ctx.Done():
	case err := <-failures:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			runErr = err
		}
	}
	stop()
	shutdown, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		slog.Error("HTTP shutdown failed", "error", err)
		runErr = errors.Join(runErr, err)
	}
	select {
	case <-botDone:
	case <-shutdown.Done():
	}
	return runErr
}
