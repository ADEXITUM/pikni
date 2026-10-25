// Command pikni-api — HTTP-сервер «Пикни».
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // база часовых поясов вшита в бинарник: в минимальном образе её нет

	"github.com/labstack/echo/v5"

	"github.com/ADEXITUM/pikni/api/internal/app"
	"github.com/ADEXITUM/pikni/api/internal/platform/db"
	"github.com/ADEXITUM/pikni/api/internal/platform/httpx"
	"github.com/ADEXITUM/pikni/api/internal/platform/logging"
	"github.com/ADEXITUM/pikni/api/internal/platform/realtime"
	"github.com/ADEXITUM/pikni/api/internal/platform/telemetry"
)

// Подставляется при сборке: -ldflags "-X main.version=1.2.3".
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "pikni-api:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := app.LoadConfig()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tel, err := telemetry.Setup(ctx, telemetry.Config{
		ServiceName:    "pikni-api",
		ServiceVersion: version,
		Environment:    cfg.Env,
		Endpoint:       cfg.OTLPEndpoint,
	})
	if err != nil {
		return err
	}
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tel.Shutdown(flushCtx)
	}()

	log := logging.New(os.Stdout, cfg.LogFormat, cfg.LogLevel, tel.LogHandler)
	slog.SetDefault(log)
	if tel.Enabled() {
		log.Info("telemetry enabled", "endpoint", cfg.OTLPEndpoint)
	}

	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	// Сервер стартует и без базы: пока она недоступна, /readyz отвечает 503.
	pingCtx, cancelPing := context.WithTimeout(ctx, 3*time.Second)
	if err := pool.Ping(pingCtx); err != nil {
		log.Warn("database is not reachable yet", "error", err)
	}
	cancelPing()

	e := httpx.NewServer(log, "pikni-api")
	httpx.RegisterHealth(e, version)
	httpx.RegisterReady(e, log, map[string]httpx.Check{"database": pool.Ping})

	hub := realtime.NewHub(pool, log)
	go hub.Run(ctx)
	if err := app.Register(e, pool, cfg.EgaisURL, hub); err != nil {
		return err
	}
	go app.Reservations(pool).Run(ctx, time.Minute, log)
	if cfg.EgaisURL != "" {
		go app.Exchanger(pool, cfg.EgaisURL, log).Run(ctx, cfg.EgaisInterval)
		log.Info("egais exchange started", "utm", cfg.EgaisURL)
	}

	sc := echo.StartConfig{
		Address:         cfg.HTTPAddr,
		HideBanner:      true,
		HidePort:        true,
		GracefulTimeout: cfg.ShutdownTimeout,
		ListenerAddrFunc: func(addr net.Addr) {
			log.Info("server started", "addr", addr.String(), "env", cfg.Env, "version", version)
		},
	}
	if err := sc.Start(ctx, e); err != nil {
		return fmt.Errorf("http server: %w", err)
	}

	log.Info("server stopped")
	return nil
}
