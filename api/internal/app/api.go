package app

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/audit"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/documents"
	"github.com/ADEXITUM/pikni/api/internal/egais"
	"github.com/ADEXITUM/pikni/api/internal/exchange"
	"github.com/ADEXITUM/pikni/api/internal/locations"
	"github.com/ADEXITUM/pikni/api/internal/marks"
	"github.com/ADEXITUM/pikni/api/internal/stock"
	"github.com/ADEXITUM/pikni/api/internal/users"
)

// Псевдонимы нужны, чтобы встроить обработчики разных модулей с одинаковым
// именем типа Handler: имя встроенного поля берётся из имени типа.
type (
	auditHandler     = audit.Handler
	authHandler      = auth.Handler
	catalogHandler   = catalog.Handler
	documentsHandler = documents.Handler
	locationsHandler = locations.Handler
	marksHandler     = marks.Handler
	stockHandler     = stock.Handler
	usersHandler     = users.Handler
)

// API собирает обработчики модулей в одну реализацию сгенерированного интерфейса.
type API struct {
	*auditHandler
	*authHandler
	*catalogHandler
	*documentsHandler
	*locationsHandler
	*marksHandler
	*stockHandler
	*usersHandler
}

var _ apispec.StrictServerInterface = (*API)(nil)

// Exchanger собирает обмен с ЕГАИС: отправку очереди и приём сообщений УТМ точек.
func Exchanger(pool *pgxpool.Pool, url string, log *slog.Logger) *exchange.Exchanger {
	docs := documents.NewService(pool, catalog.NewService(pool), marks.NewService(pool))
	return exchange.New(pool, egais.NewGovsim(url), docs, log)
}

func Register(e *echo.Echo, pool *pgxpool.Pool) error {
	sessions := auth.NewService(pool)
	authorize, err := sessions.Middleware()
	if err != nil {
		return err
	}
	cat := catalog.NewService(pool)
	mk := marks.NewService(pool)
	api := &API{
		auditHandler:     audit.NewHandler(pool),
		authHandler:      auth.NewHandler(sessions),
		catalogHandler:   catalog.NewHandler(cat),
		documentsHandler: documents.NewHandler(documents.NewService(pool, cat, mk)),
		locationsHandler: locations.NewHandler(locations.NewService(pool)),
		marksHandler:     marks.NewHandler(mk),
		stockHandler:     stock.NewHandler(stock.NewService(pool, mk)),
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
