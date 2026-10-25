package app

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
)

// docsPage — интерактивная документация API (ИН-1): Swagger UI читает контракт с сервера.
const docsPage = `<!doctype html>
<html lang="ru">
<head><meta charset="utf-8"><title>API «Пикни»</title><meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.17.14/swagger-ui.css"></head>
<body><div id="ui"></div>
<script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.17.14/swagger-ui-bundle.js"></script>
<script>SwaggerUIBundle({ url: "/api/v1/openapi.json", dom_id: "#ui" })</script>
</body>
</html>`

// apiDocs подключает контракт OpenAPI и страницу документации. Контракт открыт без входа: в нём нет
// данных, а внешней системе он нужен до получения ключа.
func apiDocs(e *echo.Echo) {
	e.GET("/api/v1/openapi.json", func(c *echo.Context) error {
		spec, err := apispec.GetSpec()
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, spec)
	})
	e.GET("/api/docs", func(c *echo.Context) error {
		return c.HTML(http.StatusOK, docsPage)
	})
}
