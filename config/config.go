package config

import "time"

type Config struct {
	Version         string        `env:"APP_VERSION" envDefault:"v0.0.1"`
	Port            int           `env:"APP_PORT" envDefault:"8080"`
	Env             string        `env:"ENV" envDefault:"development"`
	InternalToken   string        `env:"INTERNAL_TOKEN" envDefault:"local-development-token"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"10s"`

	DB DBConfig `envPrefix:"DB_"`
}

type DBConfig struct {
	DSN             string        `env:"DSN" envDefault:"postgres://finote:finote@localhost:7432/finote?sslmode=disable"`
	MaxOpenConns    int           `env:"MAX_OPEN_CONNS" envDefault:"25"`
	MinIdleConns    int           `env:"MIN_IDLE_CONNS" envDefault:"2"`
	ConnMaxLifetime time.Duration `env:"CONN_MAX_LIFETIME" envDefault:"30m"`
	ConnMaxIdleTime time.Duration `env:"CONN_MAX_IDLE_TIME" envDefault:"5m"`
}
