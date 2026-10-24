package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/audit"
)

const (
	CookieName = "pikni_sid"
	CSRFHeader = "X-CSRF-Token"

	// Значение x-permission для операций, которым достаточно входа. Только они
	// доступны, пока не сменён временный пароль.
	anyUser = "authenticated"
)

type operation struct {
	public     bool
	permission apispec.Permission
}

type ctxKey int

const (
	sessionKey ctxKey = iota
	clientKey
)

func FromContext(ctx context.Context) (Session, bool) {
	s, ok := ctx.Value(sessionKey).(Session)
	return s, ok
}

// WithSession кладёт сессию в контекст: так сервисы видят пользователя в тестах и в фоновых
// задачах, которые действуют от его имени.
func WithSession(ctx context.Context, s Session) context.Context {
	return context.WithValue(ctx, sessionKey, s)
}

func ClientFrom(ctx context.Context) Client {
	c, _ := ctx.Value(clientKey).(Client)
	return c
}

// Middleware проверяет каждый запрос по контракту: security: [] — операция открыта,
// иначе нужны сессия и право из x-permission.
func (s *Service) Middleware() (apispec.StrictMiddlewareFunc, error) {
	ops, err := loadOperations()
	if err != nil {
		return nil, fmt.Errorf("load operations from spec: %w", err)
	}

	return func(next apispec.StrictHandlerFunc, operationID string) apispec.StrictHandlerFunc {
		// Strict-сервер передаёт имя в Go-стиле (GetMe), в контракте оно getMe.
		op, known := ops[strings.ToLower(operationID)]

		return func(c *echo.Context, request any) (any, error) {
			if !known {
				return nil, fmt.Errorf("operation %s is not in the spec", operationID)
			}
			req := c.Request()
			client := Client{IP: c.RealIP(), UserAgent: req.UserAgent()}
			ctx := context.WithValue(req.Context(), clientKey, client)

			if !op.public {
				sess, err := s.sessionFor(ctx, c)
				if err != nil {
					return nil, err
				}
				if err := authorize(sess, op); err != nil {
					return nil, err
				}
				ctx = context.WithValue(ctx, sessionKey, sess)
				ctx = audit.WithActor(ctx, audit.Actor{UserID: sess.UserID, SessionID: sess.ID, IP: client.IP})
			}

			c.SetRequest(req.WithContext(ctx))
			return next(c, request)
		}
	}, nil
}

func loadOperations() (map[string]operation, error) {
	spec, err := apispec.GetSpec()
	if err != nil {
		return nil, err
	}

	ops := map[string]operation{}
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			var o operation
			if op.Security != nil && len(*op.Security) == 0 {
				o.public = true
			} else {
				p, _ := op.Extensions["x-permission"].(string)
				switch {
				case p == anyUser:
				case apispec.Permission(p).Valid():
					o.permission = apispec.Permission(p)
				default:
					return nil, fmt.Errorf("%s %s: x-permission %q is missing or unknown", method, path, p)
				}
			}
			ops[strings.ToLower(op.OperationID)] = o
		}
	}
	return ops, nil
}

func (s *Service) sessionFor(ctx context.Context, c *echo.Context) (Session, error) {
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
	return sess, nil
}

func authorize(sess Session, op operation) error {
	if op.permission == "" {
		return nil
	}
	if sess.MustChangePassword {
		return echo.NewHTTPError(http.StatusForbidden, "Сначала смените временный пароль")
	}
	if scopeOf(sess.Role, op.permission) == scopeNone {
		return echo.ErrForbidden
	}
	return nil
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
