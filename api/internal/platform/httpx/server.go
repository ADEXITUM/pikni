// Package httpx — общая настройка HTTP-сервера на Echo: middleware, журнал запросов, служебные маршруты.
package httpx

import (
	"log/slog"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func NewServer(log *slog.Logger) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.HTTPErrorHandler = errorHandler

	e.Use(middleware.RequestID())
	e.Use(requestLogger(log))
	e.Use(middleware.Recover())

	return e
}

func requestLogger(log *slog.Logger) echo.MiddlewareFunc {
	return middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogMethod:    true,
		LogURI:       true,
		LogStatus:    true,
		LogLatency:   true,
		LogRequestID: true,
		LogError:     true,
		// Без этого при ошибке в журнал попадёт статус 200: ответ ещё не записан
		// обработчиком ошибок Echo.
		HandleError: true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
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
