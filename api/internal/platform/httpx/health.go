package httpx

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

func RegisterHealth(e *echo.Echo, version string) {
	e.GET("/healthz", func(c echo.Context) error {
		return c.JSON(http.StatusOK, healthResponse{Status: "ok", Version: version})
	})
}
