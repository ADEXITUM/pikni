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

	DatabaseURL         string `env:"DATABASE_URL" envDefault:"postgres://pikni_app:pikni_app@localhost:5432/pikni?sslmode=disable"`
	DatabaseMigratorURL string `env:"DATABASE_MIGRATOR_URL" envDefault:"postgres://pikni_owner:pikni_owner@localhost:5432/pikni?sslmode=disable"`

	OTLPEndpoint string `env:"OTEL_EXPORTER_OTLP_ENDPOINT"`

	// УТМ ЕГАИС: на стенде — имитатор govsim. Пусто — обмен с ЕГАИС выключен.
	EgaisURL      string        `env:"EGAIS_URL" envDefault:"http://localhost:8090"`
	EgaisInterval time.Duration `env:"EGAIS_INTERVAL" envDefault:"3s"`
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
