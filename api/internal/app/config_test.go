package app

import (
	"log/slog"
	"testing"
	"time"
)

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.Env != "dev" {
		t.Errorf("Env = %q, want dev", cfg.Env)
	}
	if cfg.HTTPAddr != "localhost:8080" {
		t.Errorf("HTTPAddr = %q, want localhost:8080", cfg.HTTPAddr)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want INFO", cfg.LogLevel)
	}
	if cfg.ShutdownTimeout != 15*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 15s", cfg.ShutdownTimeout)
	}
}

func TestLoadConfigFromEnv(t *testing.T) {
	t.Setenv("PIKNI_ENV", "demo")
	t.Setenv("HTTP_ADDR", ":9090")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("SHUTDOWN_TIMEOUT", "3s")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Env != "demo" || cfg.HTTPAddr != ":9090" || cfg.LogLevel != slog.LevelDebug || cfg.ShutdownTimeout != 3*time.Second {
		t.Errorf("unexpected config: %+v", cfg)
	}
}

func TestMigratorURLFallsBackToDatabaseURL(t *testing.T) {
	cfg := Config{DatabaseURL: "postgres://app"}
	if got := cfg.MigratorURL(); got != "postgres://app" {
		t.Errorf("MigratorURL() = %q, want DatabaseURL", got)
	}

	cfg.DatabaseMigratorURL = "postgres://owner"
	if got := cfg.MigratorURL(); got != "postgres://owner" {
		t.Errorf("MigratorURL() = %q, want DatabaseMigratorURL", got)
	}
}

func TestLoadConfigRejectsUnknownMode(t *testing.T) {
	t.Setenv("PIKNI_ENV", "production")

	if _, err := LoadConfig(); err == nil {
		t.Fatal("LoadConfig() error = nil, want error for unknown PIKNI_ENV")
	}
}
