package users

import (
	"context"
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/auth"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

var problems = map[error]*echo.HTTPError{
	ErrNotFound:          echo.NewHTTPError(http.StatusNotFound, "Сотрудник не найден"),
	ErrLoginTaken:        echo.NewHTTPError(http.StatusConflict, "Этот логин уже занят"),
	ErrConflict:          echo.NewHTTPError(http.StatusConflict, "Данные сотрудника уже изменил другой администратор. Обновите список и повторите"),
	ErrInvalidLogin:      echo.NewHTTPError(http.StatusUnprocessableEntity, "Логин: от 3 до 32 символов — латинские буквы, цифры, точка, дефис, подчёркивание"),
	ErrEmptyName:         echo.NewHTTPError(http.StatusUnprocessableEntity, "Укажите ФИО"),
	ErrInvalidRole:       echo.NewHTTPError(http.StatusUnprocessableEntity, "Неизвестная роль"),
	ErrLocationsRequired: echo.NewHTTPError(http.StatusUnprocessableEntity, "Назначьте сотруднику хотя бы одну точку"),
	ErrNetworkLocations:  echo.NewHTTPError(http.StatusUnprocessableEntity, "Ролям уровня сети точки не назначаются"),
	ErrUnknownLocation:   echo.NewHTTPError(http.StatusUnprocessableEntity, "Такой точки нет"),
	ErrSelfLockout:       echo.NewHTTPError(http.StatusUnprocessableEntity, "Нельзя заблокировать себя или снять с себя роль администратора"),
}

func problem(err error) error {
	for target, he := range problems {
		if errors.Is(err, target) {
			return he
		}
	}
	return err
}

func actor(ctx context.Context) auth.Session {
	sess, _ := auth.FromContext(ctx)
	return sess
}

func (h *Handler) ListUsers(ctx context.Context, _ apispec.ListUsersRequestObject) (apispec.ListUsersResponseObject, error) {
	users, err := h.svc.List(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]apispec.User, len(users))
	for i, u := range users {
		items[i] = toAPI(u)
	}
	return apispec.ListUsers200JSONResponse{Items: items}, nil
}

func (h *Handler) CreateUser(ctx context.Context, req apispec.CreateUserRequestObject) (apispec.CreateUserResponseObject, error) {
	b := req.Body
	user, temporary, err := h.svc.Create(ctx, b.Login, Profile{FullName: b.FullName, Role: b.Role, LocationIDs: b.LocationIDs})
	if err != nil {
		return nil, problem(err)
	}
	return apispec.CreateUser201JSONResponse{User: toAPI(user), TemporaryPassword: temporary}, nil
}

func (h *Handler) UpdateUser(ctx context.Context, req apispec.UpdateUserRequestObject) (apispec.UpdateUserResponseObject, error) {
	b := req.Body
	p := Profile{FullName: b.FullName, Role: b.Role, LocationIDs: b.LocationIDs}
	user, err := h.svc.Update(ctx, actor(ctx).UserID, req.ID, p, b.Version)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.UpdateUser200JSONResponse(toAPI(user)), nil
}

func (h *Handler) BlockUser(ctx context.Context, req apispec.BlockUserRequestObject) (apispec.BlockUserResponseObject, error) {
	user, err := h.svc.SetBlocked(ctx, actor(ctx).UserID, req.ID, true)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.BlockUser200JSONResponse(toAPI(user)), nil
}

func (h *Handler) UnblockUser(ctx context.Context, req apispec.UnblockUserRequestObject) (apispec.UnblockUserResponseObject, error) {
	user, err := h.svc.SetBlocked(ctx, actor(ctx).UserID, req.ID, false)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.UnblockUser200JSONResponse(toAPI(user)), nil
}

func (h *Handler) ResetUserPassword(ctx context.Context, req apispec.ResetUserPasswordRequestObject) (apispec.ResetUserPasswordResponseObject, error) {
	temporary, err := h.svc.ResetPassword(ctx, req.ID)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.ResetUserPassword200JSONResponse{TemporaryPassword: temporary}, nil
}

func (h *Handler) EndUserSessions(ctx context.Context, req apispec.EndUserSessionsRequestObject) (apispec.EndUserSessionsResponseObject, error) {
	if err := h.svc.EndSessions(ctx, req.ID); err != nil {
		return nil, problem(err)
	}
	return apispec.EndUserSessions204Response{}, nil
}

func toAPI(u User) apispec.User {
	return apispec.User{
		ID:                 u.ID,
		Login:              u.Login,
		FullName:           u.FullName,
		Role:               apispec.Role(u.Role),
		LocationIDs:        u.LocationIDs,
		IsBlocked:          u.IsBlocked,
		MustChangePassword: u.MustChangePassword,
		LockedUntil:        u.LockedUntil,
		Version:            u.Version,
		CreatedAt:          u.CreatedAt,
	}
}
