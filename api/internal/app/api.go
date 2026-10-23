package app

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/audit"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/locations"
	"github.com/ADEXITUM/pikni/api/internal/users"
)

// Псевдонимы нужны, чтобы встроить обработчики разных модулей с одинаковым
// именем типа Handler: имя встроенного поля берётся из имени типа.
type (
	auditHandler     = audit.Handler
	authHandler      = auth.Handler
	locationsHandler = locations.Handler
	usersHandler     = users.Handler
)

// API собирает обработчики модулей в одну реализацию сгенерированного интерфейса.
type API struct {
	*auditHandler
	*authHandler
	*locationsHandler
	*usersHandler
}

var _ apispec.StrictServerInterface = (*API)(nil)

func Register(e *echo.Echo, pool *pgxpool.Pool) error {
	sessions := auth.NewService(pool)
	authorize, err := sessions.Middleware()
	if err != nil {
		return err
	}
	api := &API{
		auditHandler:     audit.NewHandler(pool),
		authHandler:      auth.NewHandler(sessions),
		locationsHandler: locations.NewHandler(locations.NewService(pool)),
		usersHandler:     users.NewHandler(users.NewService(pool)),
	}

	limit := auth.RateLimit()
	apispec.RegisterHandlersWithOptions(e,
		apispec.NewStrictHandler(api, []apispec.StrictMiddlewareFunc{authorize}),
		apispec.RegisterHandlersOptions{
			BaseURL: "/api/v1",
			OperationMiddlewares: map[string][]echo.MiddlewareFunc{
				"login":          {limit},
				"changePassword": {limit},
			},
		})
	return nil
}
