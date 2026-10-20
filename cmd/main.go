package main

import (
	"context"
	"errors"
	"finote/config"
	"finote/internal/infrastucture/db"
	"finote/internal/payment/httpapi"
	"finote/internal/payment/repo"
	"finote/internal/payment/service"
	"finote/internal/telegram"
	"finote/utils/env"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"
)

func main() {
	var cfg config.Config
	if err := env.Load(&cfg); err != nil {
		log.Fatalf("load config: %v", err)
	}

	database, err := db.OpenDB(cfg)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer database.Close()

	const schema = `CREATE TABLE IF NOT EXISTS payments (
		id UUID PRIMARY KEY,
		user_id TEXT NOT NULL,
		amount NUMERIC(14,2) NOT NULL CHECK (amount > 0),
		category TEXT NOT NULL,
		spent_at TIMESTAMPTZ NOT NULL,
		comment TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMPTZ NOT NULL,
		updated_at TIMESTAMPTZ NOT NULL
	)`
	if _, err := database.Exec(schema); err != nil {
		log.Fatalf("create payments table: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var bot *telegram.Bot
	if cfg.Telegram.Token != "" {
		location, err := time.LoadLocation(cfg.Telegram.Timezone)
		if err != nil {
			log.Fatalf("telegram timezone: %v", err)
		}
		agent, err := telegram.NewAgent(cfg.LLM.BaseURL, cfg.LLM.APIKey, cfg.LLM.Model)
		if err != nil {
			log.Fatal(err)
		}
		bot, err = telegram.NewBot(cfg.Telegram.Token, agent, telegram.NewStore(database, location), location)
		if err != nil {
			log.Fatal(err)
		}
	}
	handler := httpapi.New(service.New(repo.New(database)))
	address := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("payment API listening on %s", address)
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	failures := make(chan error, 2)
	go func() { failures <- server.ListenAndServe() }()
	botDone := make(chan struct{})
	if bot != nil {
		go func() { defer close(botDone); failures <- bot.Run(ctx) }()
	} else {
		close(botDone)
		log.Print("Telegram disabled: TELEGRAM_BOT_TOKEN is empty")
	}
	select {
	case <-ctx.Done():
	case err := <-failures:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("application stopped: %v", err)
		}
	}
	stop()
	shutdown, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		log.Printf("HTTP shutdown: %v", err)
	}
	select {
	case <-botDone:
	case <-shutdown.Done():
	}
}
