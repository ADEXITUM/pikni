package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
)

func get(t *testing.T, e *echo.Echo, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestHealthz(t *testing.T) {
	e := echo.New()
	RegisterHealth(e, "test")

	rec := get(t, e, "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "ok" || body.Version != "test" {
		t.Errorf("body = %+v, want status=ok version=test", body)
	}
}

func TestReadyz(t *testing.T) {
	up := func(context.Context) error { return nil }
	down := func(context.Context) error { return errors.New("connection refused") }
	silent := slog.New(slog.NewTextHandler(io.Discard, nil))

	tests := []struct {
		name       string
		checks     map[string]Check
		wantCode   int
		wantStatus string
		wantChecks map[string]string
	}{
		{
			name:       "all up",
			checks:     map[string]Check{"database": up},
			wantCode:   http.StatusOK,
			wantStatus: "ready",
			wantChecks: map[string]string{"database": "up"},
		},
		{
			name:       "database down",
			checks:     map[string]Check{"database": down},
			wantCode:   http.StatusServiceUnavailable,
			wantStatus: "not ready",
			wantChecks: map[string]string{"database": "down"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			RegisterReady(e, silent, tt.checks)

			rec := get(t, e, "/readyz")
			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantCode)
			}

			var body readyResponse
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", body.Status, tt.wantStatus)
			}
			for name, want := range tt.wantChecks {
				if got := body.Checks[name]; got != want {
					t.Errorf("checks[%s] = %q, want %q", name, got, want)
				}
			}
		})
	}
}
