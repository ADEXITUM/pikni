package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
)

const (
	CookieName = "pikni_sid"
	CSRFHeader = "X-CSRF-Token"
)

// Имена операций в том виде, в каком их передаёт сгенерированный strict-сервер.
// Операция, которой нет в publicOperations, требует сессию.
var (
	publicOperations            = map[string]bool{"Login": true}
	temporaryPasswordOperations = map[string]bool{"GetMe": true, "Logout": true, "ChangePassword": true}
)

type ctxKey int

const (
	sessionKey ctxKey = iota
	clientKey
)

func FromContext(ctx context.Context) (Session, bool) {
	s, ok := ctx.Value(sessionKey).(Session)
	return s, ok
}

func ClientFrom(ctx context.Context) Client {
	c, _ := ctx.Value(clientKey).(Client)
	return c
}

func (s *Service) Middleware(next apispec.StrictHandlerFunc, operationID string) apispec.StrictHandlerFunc {
	return func(c *echo.Context, request any) (any, error) {
		req := c.Request()
		ctx := context.WithValue(req.Context(), clientKey, Client{IP: c.RealIP(), UserAgent: req.UserAgent()})

		if !publicOperations[operationID] {
			sess, err := s.sessionFor(ctx, c, operationID)
			if err != nil {
				return nil, err
			}
			ctx = context.WithValue(ctx, sessionKey, sess)
		}

		c.SetRequest(req.WithContext(ctx))
		return next(c, request)
	}
}

func (s *Service) sessionFor(ctx context.Context, c *echo.Context, operationID string) (Session, error) {
	cookie, err := c.Cookie(CookieName)
	if err != nil {
		return Session{}, echo.ErrUnauthorized
	}
	sess, err := s.Authenticate(ctx, cookie.Value)
	if errors.Is(err, ErrNoSession) {
		return Session{}, echo.ErrUnauthorized
	}
	if err != nil {
		return Session{}, err
	}

	switch c.Request().Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
	default:
		token := c.Request().Header.Get(CSRFHeader)
		if subtle.ConstantTimeCompare([]byte(token), []byte(sess.CSRFToken)) != 1 {
			return Session{}, echo.NewHTTPError(http.StatusForbidden, "Обновите страницу и повторите действие")
		}
	}

	if sess.MustChangePassword && !temporaryPasswordOperations[operationID] {
		return Session{}, echo.NewHTTPError(http.StatusForbidden, "Сначала смените временный пароль")
	}
	return sess, nil
}

// RateLimit — 10 запросов в минуту с одного IP. Счётчики живут в памяти экземпляра,
// поэтому при двух экземплярах предел фактически вдвое выше.
func RateLimit() echo.MiddlewareFunc {
	return middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
		Store: middleware.NewRateLimiterMemoryStoreWithConfig(middleware.RateLimiterMemoryStoreConfig{
			Rate:  10.0 / 60,
			Burst: 10,
		}),
		DenyHandler: func(*echo.Context, string, error) error {
			return echo.NewHTTPError(http.StatusTooManyRequests, "Слишком много попыток с этого адреса. Подождите минуту")
		},
	})
}

func sessionCookie(token string, maxAge int) string {
	c := http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/api",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
	return c.String()
}
