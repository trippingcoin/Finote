package env

import (
	"log/slog"
	"os"

	"github.com/caarlos0/env/v10"
	"github.com/joho/godotenv"
)

func Load(target interface{}) error {
	if _, err := os.Stat(".env"); err == nil {
		if err := godotenv.Load(); err != nil {
			slog.Warn("failed to load .env file", "error", err)
		} else {
			slog.Info(".env file loaded")
		}
	} else {
		slog.Info(".env file not found, using system env variables...")
	}

	if err := env.Parse(target); err != nil {
		return err
	}
	return nil
}
