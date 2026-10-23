package httpx

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"
)

const problemContentType = "application/problem+json"

type problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}

var titles = map[int]string{
	http.StatusBadRequest:          "Некорректный запрос",
	http.StatusUnauthorized:        "Требуется вход",
	http.StatusForbidden:           "Недостаточно прав",
	http.StatusNotFound:            "Не найдено",
	http.StatusMethodNotAllowed:    "Метод не поддерживается",
	http.StatusConflict:            "Конфликт",
	http.StatusUnprocessableEntity: "Данные не прошли проверку",
	http.StatusTooManyRequests:     "Слишком много запросов",
	http.StatusInternalServerError: "Внутренняя ошибка сервера",
	http.StatusServiceUnavailable:  "Сервис временно недоступен",
}

func title(status int) string {
	if t, ok := titles[status]; ok {
		return t
	}
	return http.StatusText(status)
}

func errorHandler(c *echo.Context, err error) {
	if r, _ := echo.UnwrapResponse(c.Response()); r != nil && r.Committed {
		return
	}

	status := http.StatusInternalServerError
	var sc echo.HTTPStatusCoder
	if errors.As(err, &sc) && sc.StatusCode() != 0 {
		status = sc.StatusCode()
	}

	detail := ""
	var he *echo.HTTPError
	if errors.As(err, &he) && he.Message != http.StatusText(status) {
		detail = he.Message
	}
	// Подробности внутренних ошибок клиенту не показываем: они уже в журнале запроса.
	if status >= http.StatusInternalServerError {
		detail = ""
	}

	if c.Request().Method == http.MethodHead {
		_ = c.NoContent(status)
		return
	}
	c.Response().Header().Set(echo.HeaderContentType, problemContentType)
	_ = c.JSON(status, problem{Type: "about:blank", Title: title(status), Status: status, Detail: detail})
}
