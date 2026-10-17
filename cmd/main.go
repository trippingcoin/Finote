package main

import (
	"finote/config"
	"finote/internal/infrastucture/db"
	"finote/internal/payment/httpapi"
	"finote/internal/payment/repo"
	"finote/internal/payment/service"
	"finote/utils/env"
	"fmt"
	"log"
	"net/http"
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

	handler := httpapi.New(service.New(repo.New(database)))
	address := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("payment API listening on %s", address)
	if err := http.ListenAndServe(address, handler); err != nil {
		log.Fatalf("serve HTTP: %v", err)
	}
}
