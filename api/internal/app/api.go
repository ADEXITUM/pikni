package app

import (
	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/locations"
)

// Псевдонимы нужны, чтобы встроить обработчики разных модулей с одинаковым
// именем типа Handler: имя встроенного поля берётся из имени типа.
type locationsHandler = locations.Handler

// API собирает обработчики модулей в одну реализацию сгенерированного интерфейса.
type API struct {
	*locationsHandler
}

var _ apispec.StrictServerInterface = (*API)(nil)

func NewAPI(locs *locations.Handler) *API {
	return &API{locationsHandler: locs}
}
