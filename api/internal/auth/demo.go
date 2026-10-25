package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/auth/password"
	"github.com/ADEXITUM/pikni/api/internal/auth/store"
)

// Демонстрационный стенд (п. 4.1.4): вход под каждой ролью одной кнопкой. Учётные записи
// demo-<роль> создаются при первом входе; сотрудникам магазина назначается первый магазин сети.
// Вне режима demo операции отвечают 404.

var demoRoles = []apispec.Role{
	apispec.RoleManager, apispec.RoleAccountant, apispec.RoleStoreManager,
	apispec.RoleStorekeeper, apispec.RoleCashier, apispec.RoleAdmin,
}

var errNotDemo = echo.NewHTTPError(http.StatusNotFound, "Быстрый вход есть только на демонстрационном стенде")

func (s *Service) DemoLogin(ctx context.Context, role apispec.Role, client Client) (Session, string, error) {
	if !s.Demo {
		return Session{}, "", errNotDemo
	}
	if !role.Valid() {
		return Session{}, "", errors.New("unknown role")
	}
	login := "demo-" + string(role)
	hash, err := password.Hash(rand.Text())
	if err != nil {
		return Session{}, "", err
	}
	// Администратору второй фактор на демонстрационном стенде не нужен: вход здесь и так открыт.
	var id uuid.UUID
	err = s.pool.QueryRow(ctx, `insert into users (login, full_name, role, password_hash, must_change_password, totp_enabled)
		values ($1, $2, $3, $4, false, $3 = 'admin')
		on conflict ((lower(login))) do update set is_blocked = false, locked_until = null
		returning id`, login, "Демо: "+string(role), string(role), hash).Scan(&id)
	if err != nil {
		return Session{}, "", err
	}
	if !networkRoles[role] {
		if _, err := s.pool.Exec(ctx, `insert into user_locations (user_id, location_id)
			select $1, id from locations where kind = 'store' and is_active order by code limit 1
			on conflict do nothing`, id); err != nil {
			return Session{}, "", err
		}
	}
	u, err := s.q.GetUserByID(ctx, id)
	if err != nil {
		return Session{}, "", err
	}
	return s.startSession(ctx, store.GetUserByLoginRow(u), client, apispec.AuditActionAuthLogin)
}

func (h *Handler) ListDemoRoles(_ context.Context, _ apispec.ListDemoRolesRequestObject) (apispec.ListDemoRolesResponseObject, error) {
	if !h.svc.Demo {
		return nil, errNotDemo
	}
	return apispec.ListDemoRoles200JSONResponse{Roles: demoRoles}, nil
}

func (h *Handler) DemoLogin(ctx context.Context, req apispec.DemoLoginRequestObject) (apispec.DemoLoginResponseObject, error) {
	sess, token, err := h.svc.DemoLogin(ctx, req.Role, ClientFrom(ctx))
	if err != nil {
		return nil, err
	}
	me, cookie, err := h.signedIn(ctx, sess, token)
	if err != nil {
		return nil, err
	}
	return apispec.DemoLogin200JSONResponse{Body: me, Headers: apispec.DemoLogin200ResponseHeaders{SetCookie: &cookie}}, nil
}
