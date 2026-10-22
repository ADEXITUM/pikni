package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

type Check func(ctx context.Context) error

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

type readyResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

const readyTimeout = 2 * time.Second

func RegisterHealth(e *echo.Echo, version string) {
	e.GET("/healthz", func(c echo.Context) error {
		return c.JSON(http.StatusOK, healthResponse{Status: "ok", Version: version})
	})
}

func RegisterReady(e *echo.Echo, log *slog.Logger, checks map[string]Check) {
	e.GET("/readyz", func(c echo.Context) error {
		ctx, cancel := context.WithTimeout(c.Request().Context(), readyTimeout)
		defer cancel()

		resp := readyResponse{Status: "ready", Checks: make(map[string]string, len(checks))}
		code := http.StatusOK

		for name, check := range checks {
			if err := check(ctx); err != nil {
				log.Warn("readiness check failed", "check", name, "error", err)
				resp.Checks[name] = "down"
				resp.Status = "not ready"
				code = http.StatusServiceUnavailable
				continue
			}
			resp.Checks[name] = "up"
		}

		return c.JSON(code, resp)
	})
}
