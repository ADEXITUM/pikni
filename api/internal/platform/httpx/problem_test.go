package httpx

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/labstack/echo/v5"
)

func TestErrorsAreProblemJSON(t *testing.T) {
	e := NewServer(slog.New(slog.NewTextHandler(io.Discard, nil)), "test")
	e.GET("/panic", func(*echo.Context) error { panic("db password is hunter2") })
	e.GET("/conflict", func(*echo.Context) error {
		return echo.NewHTTPError(http.StatusConflict, "Документ уже проведён")
	})

	tests := []struct {
		path       string
		wantStatus int
		wantTitle  string
		wantDetail string
	}{
		{"/nope", http.StatusNotFound, "Не найдено", ""},
		{"/conflict", http.StatusConflict, "Конфликт", "Документ уже проведён"},
		{"/panic", http.StatusInternalServerError, "Внутренняя ошибка сервера", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			rec := get(t, e, tt.path)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if ct := rec.Header().Get(echo.HeaderContentType); ct != problemContentType {
				t.Errorf("content-type = %q, want %q", ct, problemContentType)
			}

			var p problem
			if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if p.Status != tt.wantStatus || p.Title != tt.wantTitle || p.Detail != tt.wantDetail {
				t.Errorf("problem = %+v, want status=%d title=%q detail=%q", p, tt.wantStatus, tt.wantTitle, tt.wantDetail)
			}
		})
	}
}
