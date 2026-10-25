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
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/pquerna/otp/totp"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/app"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/auth/password"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
	"github.com/ADEXITUM/pikni/api/internal/platform/httpx"
	"github.com/ADEXITUM/pikni/api/internal/platform/realtime"
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
	if err := app.Register(e, db.App, "http://127.0.0.1:1", realtime.NewHub(db.App, slog.Default())); err != nil {
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
	// Администратор работает только со вторым фактором (ПБ-3).
	b.do(http.MethodGet, "/locations", nil, http.StatusForbidden)
	recovery := b.enrollTOTP("admin")
	b.do(http.MethodGet, "/locations", nil, http.StatusOK)
	if len(recovery) != 10 {
		t.Fatalf("recovery codes: %v", recovery)
	}

	// Вход на смартфоне по QR-коду с ПК (ПБ-2): код одноразовый.
	var qr struct{ Code string }
	if err := json.NewDecoder(b.do(http.MethodPost, "/auth/qr", nil, http.StatusOK).Body).Decode(&qr); err != nil {
		t.Fatal(err)
	}
	phone := &browser{t: t, e: b.e, header: http.Header{}}
	rec = phone.do(http.MethodPost, "/auth/qr/redeem", map[string]string{"code": qr.Code}, http.StatusOK)
	phone.cookie = sessionCookie(t, rec)
	phone.do(http.MethodGet, "/locations", nil, http.StatusOK)
	phone.send(http.MethodPost, "/auth/qr/redeem", map[string]string{"code": qr.Code})
	if r := phone.send(http.MethodPost, "/auth/qr/redeem", map[string]string{"code": qr.Code}); r.Code != http.StatusUnauthorized {
		t.Fatalf("second redeem: %d", r.Code)
	}

	// Повторный вход — в два шага: пароль, затем код; резервный код тоже подходит.
	again := &browser{t: t, e: b.e, header: http.Header{}}
	rec = again.do(http.MethodPost, "/auth/login", map[string]string{"login": "admin", "password": "новый-пароль"}, http.StatusAccepted)
	var ch apispec.LoginChallenge
	if err := json.NewDecoder(rec.Body).Decode(&ch); err != nil {
		t.Fatal(err)
	}
	again.do(http.MethodPost, "/auth/login/totp", map[string]string{"challenge": ch.Challenge, "code": "000000"}, http.StatusUnauthorized)
	again.do(http.MethodPost, "/auth/login/totp", map[string]string{"challenge": ch.Challenge, "code": recovery[0]}, http.StatusOK)

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

// secrets — секреты TOTP пользователей тестов: администратор без второго фактора не работает.
var secrets sync.Map

// login входит как сотрудник: с одноразовым кодом, если он подключён, и подключает его
// администратору, как это сделал бы человек при первом входе.
func (b *browser) login(login string) {
	b.t.Helper()
	body := map[string]string{"login": login, "password": staffPassword}
	rec := b.send(http.MethodPost, "/auth/login", body)
	if rec.Code == http.StatusAccepted {
		var ch apispec.LoginChallenge
		if err := json.NewDecoder(rec.Body).Decode(&ch); err != nil {
			b.t.Fatal(err)
		}
		secret, _ := secrets.Load(login)
		code, _ := totp.GenerateCode(secret.(string), time.Now())
		rec = b.do(http.MethodPost, "/auth/login/totp", map[string]string{"challenge": ch.Challenge, "code": code}, http.StatusOK)
	} else if rec.Code != http.StatusOK {
		b.t.Fatalf("login %s: %d %s", login, rec.Code, rec.Body)
	}
	var me apispec.Me
	if err := json.NewDecoder(rec.Body).Decode(&me); err != nil {
		b.t.Fatal(err)
	}
	b.cookie, b.csrf = sessionCookie(b.t, rec), me.CsrfToken
	if me.MustEnrollTotp != nil && *me.MustEnrollTotp {
		b.enrollTOTP(login)
	}
}

func (b *browser) enrollTOTP(login string) []string {
	b.t.Helper()
	var setup struct{ Secret string }
	if err := json.NewDecoder(b.do(http.MethodPost, "/auth/totp/setup", nil, http.StatusOK).Body).Decode(&setup); err != nil {
		b.t.Fatal(err)
	}
	code, _ := totp.GenerateCode(setup.Secret, time.Now())
	var enabled struct{ RecoveryCodes []string }
	if err := json.NewDecoder(b.do(http.MethodPost, "/auth/totp/enable", map[string]string{"code": code}, http.StatusOK).Body).Decode(&enabled); err != nil {
		b.t.Fatal(err)
	}
	secrets.Store(login, setup.Secret)
	return enabled.RecoveryCodes
}

// send — запрос без проверки статуса.
func (b *browser) send(method, path string, body any) *httptest.ResponseRecorder {
	b.t.Helper()
	data, _ := json.Marshal(body)
	req := httptest.NewRequestWithContext(b.t.Context(), method, "/api/v1"+path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	b.e.ServeHTTP(rec, req)
	return rec
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
	// Последнее событие — подключение второго фактора самим администратором, за ним есть ещё страницы.
	if len(page.Items) != 1 || page.Items[0].Action != apispec.AuditActionAuthTotpEnabled ||
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
		insert into locations (code, name, kind, city, timezone, subject_code)
		values ('МГ-01', 'Магазин 1', 'store', 'Благовещенск', 'Asia/Yakutsk', '28') returning id`).Scan(&store)
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

func TestLocationPermissions(t *testing.T) {
	t.Parallel()
	boss, db := newBrowser(t)
	createStaff(t, db, "boss", apispec.RoleManager)
	kassir := &browser{t: t, e: boss.e, header: http.Header{}}
	createStaff(t, db, "kassir", apispec.RoleCashier)

	location := map[string]any{
		"code": "МГ-05", "kind": "store", "name": "Магазин", "city": "Благовещенск",
		"timezone": "Asia/Yakutsk", "subjectCode": "28", "openingHours": map[string]any{},
	}
	boss.login("boss")
	rec := boss.do(http.MethodPost, "/locations", location, http.StatusCreated)
	var created apispec.Location
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if len(created.Zones) != 2 {
		t.Errorf("zones = %+v", created.Zones)
	}

	kassir.login("kassir")
	kassir.do(http.MethodGet, "/locations/"+created.ID.String(), nil, http.StatusOK)
	kassir.do(http.MethodPost, "/locations", location, http.StatusForbidden)
	kassir.do(http.MethodGet, "/locations/"+uuid.NewString(), nil, http.StatusNotFound)
	kassir.do(http.MethodGet, "/locations/"+created.ID.String()+"/sale-check", nil, http.StatusOK)
	kassir.do(http.MethodGet, "/sale-rules", nil, http.StatusOK)
	kassir.do(http.MethodPost, "/sale-rules", map[string]any{
		"scope": "all", "validFrom": "2030-01-01", "from": "10:00", "to": "20:00", "source": "x",
	}, http.StatusForbidden)
}

func TestMeShowsWorkLocations(t *testing.T) {
	t.Parallel()
	b, db := newBrowser(t)
	ctx := t.Context()

	var store uuid.UUID
	err := db.App.QueryRow(ctx, `
		insert into locations (code, name, kind, city, timezone, subject_code) values
			('МГ-01', 'Магазин 1', 'store', 'Благовещенск', 'Asia/Yakutsk', '28'),
			('МГ-02', 'Магазин 2', 'store', 'Хабаровск', 'Asia/Vladivostok', '28')
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

func TestAPIKeys(t *testing.T) {
	t.Parallel()
	admin, db := newBrowser(t)
	createStaff(t, db, "admin", apispec.RoleAdmin)
	admin.login("admin")
	rec := admin.do(http.MethodPost, "/api-keys", map[string]any{"name": "Сайт сети", "scopes": []string{"availability"}}, http.StatusCreated)
	var created struct {
		Key    string
		APIKey apispec.APIKey `json:"apiKey"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	site := &browser{t: t, e: admin.e, header: http.Header{"Authorization": {"Bearer " + created.Key}}}
	site.do(http.MethodGet, "/availability", nil, http.StatusOK)
	site.do(http.MethodGet, "/products", nil, http.StatusForbidden)
	site.do(http.MethodGet, "/users", nil, http.StatusForbidden)
	forged := &browser{t: t, e: admin.e, header: http.Header{"Authorization": {"Bearer " + created.Key + "x"}}}
	forged.do(http.MethodGet, "/availability", nil, http.StatusUnauthorized)

	var log struct{ Items []apispec.APIKeyRequest }
	if err := json.NewDecoder(admin.do(http.MethodGet, "/api-keys/"+created.APIKey.ID.String()+"/requests", nil, http.StatusOK).Body).Decode(&log); err != nil {
		t.Fatal(err)
	}
	if len(log.Items) != 3 || log.Items[2].Operation != "searchavailability" || !log.Items[2].Ok || log.Items[0].Ok {
		t.Fatalf("key log: %+v", log.Items)
	}
	admin.do(http.MethodDelete, "/api-keys/"+created.APIKey.ID.String(), nil, http.StatusNoContent)
	site.do(http.MethodGet, "/availability", nil, http.StatusUnauthorized)
}
