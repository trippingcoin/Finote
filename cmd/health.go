package main

import (
	"context"
	"database/sql"
	"finote/config"
	"finote/internal/telegram"
	"fmt"
	"net/http"
	"time"
)

type pinger interface{ PingContext(context.Context) error }

// Readiness describes the panel/API and database, not external Telegram or LLM.
func healthHandler(next http.Handler, db pinger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprintln(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintln(w, `{"status":"unavailable","database":"unavailable"}`)
			return
		}
		fmt.Fprintln(w, `{"status":"ready","database":"ok"}`)
	})
	mux.Handle("/", next)
	return mux
}

func configureBot(cfg config.Config, db *sql.DB) (*telegram.Bot, error) {
	loc, err := time.LoadLocation(cfg.Telegram.Timezone)
	if err != nil {
		return nil, fmt.Errorf("telegram timezone: %w", err)
	}
	agent, err := telegram.NewAgent(cfg.LLM.BaseURL, cfg.LLM.APIKey, cfg.LLM.Model)
	if err != nil {
		return nil, fmt.Errorf("initialize LLM agent: %w", err)
	}
	return telegram.NewBot(cfg.Telegram.Token, agent, telegram.NewStore(db, loc), loc)
}
