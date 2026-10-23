package app_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/app"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/auth/password"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
	"github.com/ADEXITUM/pikni/api/internal/platform/httpx"
	"github.com/ADEXITUM/pikni/api/internal/users"
)

// browser хранит cookie и CSRF-токен между запросами, как это делает фронт.
type browser struct {
	t      *testing.T
	e      *echo.Echo
	cookie *http.Cookie
	csrf   string
	header http.Header
}

func newBrowser(t *testing.T) (*browser, *dbtest.DB) {
	t.Helper()
	db := dbtest.New(t)
	e := httpx.NewServer(slog.New(slog.DiscardHandler), "pikni-test")
	if err := app.Register(e, db.App); err != nil {
		t.Fatal(err)
	}
	return &browser{t: t, e: e, header: http.Header{}}, db
}

func (b *browser) do(method, path string, body any, wantStatus int) *httptest.ResponseRecorder {
	b.t.Helper()
	var r io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			b.t.Fatal(err)
		}
		r = bytes.NewReader(data)
	}

	req := httptest.NewRequestWithContext(b.t.Context(), method, "/api/v1"+path, r)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range b.header {
		req.Header[k] = v
	}
	if b.cookie != nil {
		req.AddCookie(b.cookie)
	}
	if b.csrf != "" {
		req.Header.Set(auth.CSRFHeader, b.csrf)
	}

	rec := httptest.NewRecorder()
	b.e.ServeHTTP(rec, req)
	if rec.Code != wantStatus {
		b.t.Fatalf("%s %s: status = %d, want %d; body: %s", method, path, rec.Code, wantStatus, rec.Body)
	}
	return rec
}

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName {
			return c
		}
	}
	t.Fatal("no session cookie in response")
	return nil
}

func TestAuthFlow(t *testing.T) {
	t.Parallel()
	b, db := newBrowser(t)
	temp, err := users.NewService(db.App).CreateAdmin(t.Context(), "admin", "Администратор")
	if err != nil {
		t.Fatal(err)
	}

	b.do(http.MethodGet, "/locations", nil, http.StatusUnauthorized)
	rec := b.do(http.MethodPost, "/auth/login", map[string]string{"login": "admin", "password": "неверный"}, http.StatusUnauthorized)
	if !strings.Contains(rec.Body.String(), "Неверный логин или пароль") {
		t.Errorf("login error body = %s", rec.Body)
	}

	rec = b.do(http.MethodPost, "/auth/login", map[string]string{"login": "admin", "password": temp}, http.StatusOK)
	b.cookie = sessionCookie(t, rec)
	if !b.cookie.HttpOnly || !b.cookie.Secure || b.cookie.SameSite != http.SameSiteLaxMode || b.cookie.Path != "/api" {
		t.Errorf("cookie attributes: %+v", b.cookie)
	}
	var me apispec.Me
	if err := json.NewDecoder(rec.Body).Decode(&me); err != nil {
		t.Fatal(err)
	}
	if !me.MustChangePassword || me.CsrfToken == "" || me.Role != apispec.RoleAdmin ||
		!slices.Contains(me.Permissions, apispec.PermissionUsersManage) {
		t.Errorf("me = %+v", me)
	}

	b.do(http.MethodGet, "/locations", nil, http.StatusForbidden)
	b.do(http.MethodGet, "/auth/me", nil, http.StatusOK)

	change := map[string]string{"currentPassword": temp, "newPassword": "новый-пароль"}
	b.do(http.MethodPost, "/auth/password", change, http.StatusForbidden)
	b.csrf = me.CsrfToken
	b.do(http.MethodPost, "/auth/password", change, http.StatusNoContent)
	b.do(http.MethodGet, "/locations", nil, http.StatusOK)

	rec = b.do(http.MethodPost, "/auth/logout", nil, http.StatusNoContent)
	if c := sessionCookie(t, rec); c.MaxAge >= 0 {
		t.Errorf("logout cookie MaxAge = %d, want deletion", c.MaxAge)
	}
	b.do(http.MethodGet, "/auth/me", nil, http.StatusUnauthorized)
}

func TestMeShowsWorkLocations(t *testing.T) {
	t.Parallel()
	b, db := newBrowser(t)
	ctx := t.Context()

	hash, err := password.Hash("пароль-сотрудника")
	if err != nil {
		t.Fatal(err)
	}
	var store uuid.UUID
	err = db.App.QueryRow(ctx, `
		with locs as (
			insert into locations (code, name, kind, city, timezone) values
				('МГ-01', 'Магазин 1', 'store', 'Благовещенск', 'Asia/Yakutsk'),
				('МГ-02', 'Магазин 2', 'store', 'Хабаровск', 'Asia/Vladivostok')
			returning id, code
		), staff as (
			insert into users (login, full_name, role, password_hash, must_change_password)
			values ('kassir', 'Кассир', 'cashier', $1, false), ('boss', 'Руководитель', 'manager', $1, false)
			returning id, role
		), bind as (
			insert into user_locations (user_id, location_id)
			select staff.id, locs.id from staff, locs where staff.role = 'cashier' and locs.code = 'МГ-01'
		)
		select id from locs where code = 'МГ-01'`, hash).Scan(&store)
	if err != nil {
		t.Fatalf("prepare data: %v", err)
	}

	tests := []struct {
		login     string
		locations int
		sells     bool
	}{
		{"kassir", 1, true},
		{"boss", 2, false},
	}
	for _, tt := range tests {
		rec := b.do(http.MethodPost, "/auth/login", map[string]string{"login": tt.login, "password": "пароль-сотрудника"}, http.StatusOK)
		var me apispec.Me
		if err := json.NewDecoder(rec.Body).Decode(&me); err != nil {
			t.Fatal(err)
		}
		if len(me.LocationIDs) != tt.locations || slices.Contains(me.Permissions, apispec.PermissionPosSell) != tt.sells {
			t.Errorf("%s: locationIds=%v permissions=%v", tt.login, me.LocationIDs, me.Permissions)
		}
		if tt.login == "kassir" && me.LocationIDs[0] != store {
			t.Errorf("cashier location = %s, want %s", me.LocationIDs[0], store)
		}

		b.cookie = sessionCookie(t, rec)
		b.do(http.MethodGet, "/locations", nil, http.StatusOK)
	}
}

func TestLoginRateLimit(t *testing.T) {
	t.Parallel()
	b, _ := newBrowser(t)
	wrong := map[string]string{"login": "nobody", "password": "неверный"}

	for range 10 {
		b.do(http.MethodPost, "/auth/login", wrong, http.StatusUnauthorized)
	}
	b.do(http.MethodPost, "/auth/login", wrong, http.StatusTooManyRequests)
}

func TestCrossSiteRequestRejected(t *testing.T) {
	t.Parallel()
	b, _ := newBrowser(t)
	b.header.Set("Sec-Fetch-Site", "cross-site")

	b.do(http.MethodPost, "/auth/login", map[string]string{"login": "admin", "password": "x"}, http.StatusForbidden)
}
