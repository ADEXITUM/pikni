// Package httpx — общая настройка HTTP-сервера на Echo: middleware, журнал запросов, служебные маршруты.
package httpx

import (
	"log/slog"
	"net/http"
	"sync/atomic"

	echootel "github.com/labstack/echo-opentelemetry"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

func NewServer(log *slog.Logger, serviceName string) *echo.Echo {
	e := echo.New()
	e.Logger = log
	e.HTTPErrorHandler = errorHandler
	// За Caddy адрес клиента приходит в X-Forwarded-For. Заголовку верим, только если
	// запрос пришёл из частной сети, где стоит прокси.
	e.IPExtractor = echo.ExtractIPFromXFFHeader()

	e.Use(middleware.RequestID())
	e.Use(countInFlight)
	e.Use(echootel.NewMiddlewareWithConfig(echootel.Config{ServerName: serviceName, Skipper: isProbe}))
	e.Use(requestLogger(log))
	e.Use(middleware.Recover())
	e.Use(crossOriginProtection())

	return e
}

// InFlight — запросов в обработке сейчас: метрика pikni.http.in_flight показывает очередь под нагрузкой.
var InFlight atomic.Int64

func countInFlight(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		InFlight.Add(1)
		defer InFlight.Add(-1)
		return next(c)
	}
}

// Изменяющие запросы с чужих сайтов отклоняются по заголовкам Sec-Fetch-Site и Origin,
// которые браузер выставляет сам и которые страница подделать не может.
func crossOriginProtection() echo.MiddlewareFunc {
	cop := http.NewCrossOriginProtection()
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if err := cop.Check(c.Request()); err != nil {
				return echo.NewHTTPError(http.StatusForbidden, "Запрос с другого сайта отклонён")
			}
			return next(c)
		}
	}
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
