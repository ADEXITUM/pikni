// Package app собирает приложение «Пикни»: читает конфигурацию и связывает компоненты.
package app

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	Env             string        `env:"PIKNI_ENV" envDefault:"dev"`
	HTTPAddr        string        `env:"HTTP_ADDR" envDefault:"localhost:8080"`
	LogLevel        slog.Level    `env:"LOG_LEVEL" envDefault:"info"`
	LogFormat       string        `env:"LOG_FORMAT" envDefault:"json"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"15s"`

	DatabaseURL         string `env:"DATABASE_URL" envDefault:"postgres://pikni:pikni@localhost:5432/pikni?sslmode=disable"`
	DatabaseMigratorURL string `env:"DATABASE_MIGRATOR_URL"`
}

func (c Config) MigratorURL() string {
	if c.DatabaseMigratorURL != "" {
		return c.DatabaseMigratorURL
	}
	return c.DatabaseURL
}

func LoadConfig() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	switch cfg.Env {
	case "dev", "demo", "prod":
	default:
		return Config{}, fmt.Errorf("PIKNI_ENV: unknown mode %q (want dev, demo or prod)", cfg.Env)
	}

	switch cfg.LogFormat {
	case "json", "text":
	default:
		return Config{}, fmt.Errorf("LOG_FORMAT: unknown format %q (want json or text)", cfg.LogFormat)
	}

	return cfg, nil
}
