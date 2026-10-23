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

const staffPassword = "пароль-сотрудника"

func createStaff(t *testing.T, db *dbtest.DB, login string, role apispec.Role) uuid.UUID {
	t.Helper()
	hash, err := password.Hash(staffPassword)
	if err != nil {
		t.Fatal(err)
	}
	var id uuid.UUID
	err = db.App.QueryRow(t.Context(), `
		insert into users (login, full_name, role, password_hash, must_change_password)
		values ($1, $1, $2, $3, false) returning id`, login, role, hash).Scan(&id)
	if err != nil {
		t.Fatalf("create %s: %v", login, err)
	}
	return id
}

func (b *browser) login(login string) {
	b.t.Helper()
	rec := b.do(http.MethodPost, "/auth/login", map[string]string{"login": login, "password": staffPassword}, http.StatusOK)
	var me apispec.Me
	if err := json.NewDecoder(rec.Body).Decode(&me); err != nil {
		b.t.Fatal(err)
	}
	b.cookie, b.csrf = sessionCookie(b.t, rec), me.CsrfToken
}

func TestAuditLogAccess(t *testing.T) {
	t.Parallel()
	b, db := newBrowser(t)
	kassir := createStaff(t, db, "kassir", apispec.RoleCashier)
	createStaff(t, db, "admin", apispec.RoleAdmin)

	b.login("kassir")
	b.do(http.MethodGet, "/audit", nil, http.StatusForbidden)
	b.do(http.MethodPost, "/auth/logout", nil, http.StatusNoContent)

	b.login("admin")
	rec := b.do(http.MethodGet, "/audit?action=auth.login&userId="+kassir.String(), nil, http.StatusOK)
	var page apispec.AuditPage
	if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].User == nil || page.Items[0].User.Login != "kassir" {
		t.Fatalf("cashier logins = %+v", page.Items)
	}

	rec = b.do(http.MethodGet, "/audit?limit=1", nil, http.StatusOK)
	page = apispec.AuditPage{}
	if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	// Последнее событие — вход самого администратора, а за ним есть ещё страницы.
	if len(page.Items) != 1 || page.Items[0].Action != apispec.AuditActionAuthLogin ||
		page.Items[0].User.Login != "admin" || page.NextBefore == nil {
		t.Errorf("latest entry = %+v, nextBefore = %v", page.Items, page.NextBefore)
	}
}

func TestMySessions(t *testing.T) {
	t.Parallel()
	pc, db := newBrowser(t)
	createStaff(t, db, "petrova", apispec.RoleStoreManager)
	phone := &browser{t: t, e: pc.e, header: http.Header{"User-Agent": {"Phone"}}}
	pc.login("petrova")
	phone.login("petrova")

	rec := pc.do(http.MethodGet, "/auth/sessions", nil, http.StatusOK)
	var list apispec.SessionList
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	current := 0
	for _, s := range list.Items {
		if s.Current {
			current++
		}
	}
	if len(list.Items) != 2 || current != 1 {
		t.Fatalf("sessions = %+v, want 2 with one current", list.Items)
	}

	pc.do(http.MethodDelete, "/auth/sessions", nil, http.StatusNoContent)
	phone.do(http.MethodGet, "/auth/me", nil, http.StatusUnauthorized)
	pc.do(http.MethodGet, "/auth/me", nil, http.StatusOK)
	pc.do(http.MethodDelete, "/auth/sessions/"+uuid.NewString(), nil, http.StatusNotFound)
}

func TestAdminManagesStaff(t *testing.T) {
	t.Parallel()
	admin, db := newBrowser(t)
	createStaff(t, db, "admin", apispec.RoleAdmin)
	createStaff(t, db, "boss", apispec.RoleManager)
	var store uuid.UUID
	err := db.App.QueryRow(t.Context(), `
		insert into locations (code, name, kind, city, timezone)
		values ('МГ-01', 'Магазин 1', 'store', 'Благовещенск', 'Asia/Yakutsk') returning id`).Scan(&store)
	if err != nil {
		t.Fatal(err)
	}

	boss := &browser{t: t, e: admin.e, header: http.Header{}}
	boss.login("boss")
	boss.do(http.MethodGet, "/users", nil, http.StatusOK)
	boss.do(http.MethodPost, "/users", map[string]any{"login": "x1", "fullName": "X", "role": "manager", "locationIds": []uuid.UUID{}}, http.StatusForbidden)

	admin.login("admin")
	rec := admin.do(http.MethodPost, "/users", map[string]any{
		"login": "kassir", "fullName": "Кассир", "role": "cashier", "locationIds": []uuid.UUID{store},
	}, http.StatusCreated)
	var created apispec.CreatedUser
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	kassir := &browser{t: t, e: admin.e, header: http.Header{}}
	rec = kassir.do(http.MethodPost, "/auth/login", map[string]string{"login": "kassir", "password": created.TemporaryPassword}, http.StatusOK)
	kassir.cookie = sessionCookie(t, rec)

	admin.do(http.MethodPost, "/users/"+created.User.ID.String()+"/block", nil, http.StatusOK)
	kassir.do(http.MethodGet, "/auth/me", nil, http.StatusUnauthorized)
	kassir.do(http.MethodPost, "/auth/login", map[string]string{"login": "kassir", "password": created.TemporaryPassword}, http.StatusForbidden)

	admin.do(http.MethodPut, "/users/"+created.User.ID.String(), map[string]any{
		"fullName": "Кассир", "role": "cashier", "locationIds": []uuid.UUID{store}, "version": created.User.Version,
	}, http.StatusConflict)
}

func TestMeShowsWorkLocations(t *testing.T) {
	t.Parallel()
	b, db := newBrowser(t)
	ctx := t.Context()

	var store uuid.UUID
	err := db.App.QueryRow(ctx, `
		insert into locations (code, name, kind, city, timezone) values
			('МГ-01', 'Магазин 1', 'store', 'Благовещенск', 'Asia/Yakutsk'),
			('МГ-02', 'Магазин 2', 'store', 'Хабаровск', 'Asia/Vladivostok')
		returning id`).Scan(&store)
	if err != nil {
		t.Fatalf("create locations: %v", err)
	}
	kassir := createStaff(t, db, "kassir", apispec.RoleCashier)
	createStaff(t, db, "boss", apispec.RoleManager)
	if _, err := db.App.Exec(ctx, `insert into user_locations (user_id, location_id) values ($1, $2)`, kassir, store); err != nil {
		t.Fatal(err)
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
		rec := b.do(http.MethodPost, "/auth/login", map[string]string{"login": tt.login, "password": staffPassword}, http.StatusOK)
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
