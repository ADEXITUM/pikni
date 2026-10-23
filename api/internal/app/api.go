package app

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/locations"
	locstore "github.com/ADEXITUM/pikni/api/internal/locations/store"
)

// Псевдонимы нужны, чтобы встроить обработчики разных модулей с одинаковым
// именем типа Handler: имя встроенного поля берётся из имени типа.
type (
	authHandler      = auth.Handler
	locationsHandler = locations.Handler
)

// API собирает обработчики модулей в одну реализацию сгенерированного интерфейса.
type API struct {
	*authHandler
	*locationsHandler
}

var _ apispec.StrictServerInterface = (*API)(nil)

func Register(e *echo.Echo, pool *pgxpool.Pool) {
	sessions := auth.NewService(pool)
	api := &API{
		authHandler:      auth.NewHandler(sessions),
		locationsHandler: locations.NewHandler(locations.NewService(locstore.New(pool))),
	}

	limit := auth.RateLimit()
	apispec.RegisterHandlersWithOptions(e,
		apispec.NewStrictHandler(api, []apispec.StrictMiddlewareFunc{sessions.Middleware}),
		apispec.RegisterHandlersOptions{
			BaseURL: "/api/v1",
			OperationMiddlewares: map[string][]echo.MiddlewareFunc{
				"login":          {limit},
				"changePassword": {limit},
			},
		})
}
