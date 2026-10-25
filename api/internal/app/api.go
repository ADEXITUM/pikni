package app

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/audit"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/availability"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/crpt"
	"github.com/ADEXITUM/pikni/api/internal/documents"
	"github.com/ADEXITUM/pikni/api/internal/egais"
	"github.com/ADEXITUM/pikni/api/internal/exchange"
	"github.com/ADEXITUM/pikni/api/internal/locations"
	"github.com/ADEXITUM/pikni/api/internal/marks"
	"github.com/ADEXITUM/pikni/api/internal/platform/realtime"
	"github.com/ADEXITUM/pikni/api/internal/pos"
	"github.com/ADEXITUM/pikni/api/internal/posspec"
	"github.com/ADEXITUM/pikni/api/internal/reservations"
	"github.com/ADEXITUM/pikni/api/internal/stock"
	"github.com/ADEXITUM/pikni/api/internal/users"
)

// Псевдонимы нужны, чтобы встроить обработчики разных модулей с одинаковым
// именем типа Handler: имя встроенного поля берётся из имени типа.
type (
	auditHandler     = audit.Handler
	authHandler      = auth.Handler
	availHandler     = availability.Handler
	catalogHandler   = catalog.Handler
	documentsHandler = documents.Handler
	locationsHandler = locations.Handler
	marksHandler     = marks.Handler
	posStaffHandler  = pos.StaffHandler
	resHandler       = reservations.Handler
	stockHandler     = stock.Handler
	usersHandler     = users.Handler
)

// API собирает обработчики модулей в одну реализацию сгенерированного интерфейса.
type API struct {
	*auditHandler
	*authHandler
	*availHandler
	*catalogHandler
	*documentsHandler
	*locationsHandler
	*marksHandler
	*posStaffHandler
	*resHandler
	*stockHandler
	*usersHandler
}

var _ apispec.StrictServerInterface = (*API)(nil)

// Reservations — снятие просроченных броней и обезличивание данных покупателей (НЛ-4, НЛ-5).
func Reservations(pool *pgxpool.Pool) *reservations.Service {
	return reservations.NewService(pool, catalog.NewService(pool), marks.NewService(pool))
}

// Exchanger собирает обмен с ЕГАИС: отправку очереди и приём сообщений УТМ точек.
func Exchanger(pool *pgxpool.Pool, url string, log *slog.Logger) *exchange.Exchanger {
	docs := documents.NewService(pool, catalog.NewService(pool), marks.NewService(pool))
	return exchange.New(pool, egais.NewGovsim(url), docs, log)
}

// Register подключает API приложения (/api/v1) и кассовый API (/api/pos/v1). govsimURL — адрес
// УТМ ЕГАИС и «Честного знака»: на стенде их заменяет имитатор.
func Register(e *echo.Echo, pool *pgxpool.Pool, govsimURL string, hub *realtime.Hub) error {
	sessions := auth.NewService(pool)
	authorize, err := sessions.Middleware()
	if err != nil {
		return err
	}
	cat := catalog.NewService(pool)
	mk := marks.NewService(pool)
	docs := documents.NewService(pool, cat, mk)
	locs := locations.NewService(pool)
	res := reservations.NewService(pool, cat, mk)
	tills := pos.NewService(pool, cat, mk, docs, locs, egais.NewGovsim(govsimURL), crpt.NewGovsim(govsimURL), res)
	api := &API{
		auditHandler:     audit.NewHandler(pool),
		authHandler:      auth.NewHandler(sessions),
		availHandler:     availability.NewHandler(availability.NewService(pool, mk)),
		catalogHandler:   catalog.NewHandler(cat),
		documentsHandler: documents.NewHandler(docs),
		locationsHandler: locations.NewHandler(locs),
		marksHandler:     marks.NewHandler(mk),
		posStaffHandler:  pos.NewStaffHandler(tills),
		resHandler:       reservations.NewHandler(res),
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
	// События реального времени: WebSocket вне контракта OpenAPI, вход — по cookie сеанса.
	e.GET("/api/v1/ws", func(c *echo.Context) error {
		cookie, err := c.Cookie(auth.CookieName)
		if err != nil {
			return echo.ErrUnauthorized
		}
		if _, err := sessions.Authenticate(c.Request().Context(), cookie.Value); err != nil {
			return echo.ErrUnauthorized
		}
		return hub.Serve(c.Response(), c.Request())
	})
	posspec.RegisterHandlersWithOptions(e,
		posspec.NewStrictHandler(pos.NewHandler(tills), []posspec.StrictMiddlewareFunc{tills.Middleware()}),
		posspec.RegisterHandlersOptions{
			BaseURL:              "/api/pos/v1",
			OperationMiddlewares: map[string][]echo.MiddlewareFunc{"posLogin": {auth.RateLimit()}},
		})
	return nil
}
