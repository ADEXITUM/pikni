// Package httpx — общая настройка HTTP-сервера на Echo: middleware, журнал запросов, служебные маршруты.
package httpx

import (
	"log/slog"

	echootel "github.com/labstack/echo-opentelemetry"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

func NewServer(log *slog.Logger, serviceName string) *echo.Echo {
	e := echo.New()
	e.Logger = log
	e.HTTPErrorHandler = errorHandler

	e.Use(middleware.RequestID())
	e.Use(echootel.NewMiddlewareWithConfig(echootel.Config{ServerName: serviceName, Skipper: isProbe}))
	e.Use(requestLogger(log))
	e.Use(middleware.Recover())

	return e
}

func isProbe(c *echo.Context) bool {
	p := c.Request().URL.Path
	return p == "/healthz" || p == "/readyz"
}

func requestLogger(log *slog.Logger) echo.MiddlewareFunc {
	return middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		Skipper:      isProbe,
		LogMethod:    true,
		LogURI:       true,
		LogStatus:    true,
		LogLatency:   true,
		LogRequestID: true,
		// Без этого при ошибке в журнал попадёт статус 200: ответ ещё не записан
		// обработчиком ошибок Echo.
		HandleError: true,
		LogValuesFunc: func(c *echo.Context, v middleware.RequestLoggerValues) error {
			attrs := []slog.Attr{
				slog.String("method", v.Method),
				slog.String("uri", v.URI),
				slog.Int("status", v.Status),
				slog.Duration("latency", v.Latency),
				slog.String("request_id", v.RequestID),
			}
			if v.Error != nil {
				attrs = append(attrs, slog.String("error", v.Error.Error()))
			}

			level := slog.LevelInfo
			switch {
			case v.Status >= 500:
				level = slog.LevelError
			case v.Status >= 400:
				level = slog.LevelWarn
			}

			log.LogAttrs(c.Request().Context(), level, "http request", attrs...)
			return nil
		},
	})
}
