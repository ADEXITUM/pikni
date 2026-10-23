package auth

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/auth/password"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Login(ctx context.Context, req apispec.LoginRequestObject) (apispec.LoginResponseObject, error) {
	sess, token, err := h.svc.Login(ctx, req.Body.Login, req.Body.Password, ClientFrom(ctx))

	var locked *LockedError
	switch {
	case errors.Is(err, ErrInvalidCredentials):
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "Неверный логин или пароль")
	case errors.Is(err, ErrBlocked):
		return nil, echo.NewHTTPError(http.StatusForbidden, "Учётная запись заблокирована. Обратитесь к администратору")
	case errors.As(err, &locked):
		minutes := int(math.Ceil(locked.RetryAfter.Minutes()))
		return nil, echo.NewHTTPError(http.StatusTooManyRequests,
			fmt.Sprintf("Слишком много неудачных попыток. Попробуйте через %d мин.", minutes))
	case err != nil:
		return nil, err
	}

	me, err := h.me(ctx, sess)
	if err != nil {
		return nil, err
	}
	cookie := sessionCookie(token, int(sessionLifetime.Seconds()))
	return apispec.Login200JSONResponse{
		Body:    me,
		Headers: apispec.Login200ResponseHeaders{SetCookie: &cookie},
	}, nil
}

func (h *Handler) Logout(ctx context.Context, _ apispec.LogoutRequestObject) (apispec.LogoutResponseObject, error) {
	sess, _ := FromContext(ctx)
	if err := h.svc.Logout(ctx, sess); err != nil {
		return nil, err
	}
	cookie := sessionCookie("", -1)
	return apispec.Logout204Response{Headers: apispec.Logout204ResponseHeaders{SetCookie: &cookie}}, nil
}

func (h *Handler) GetMe(ctx context.Context, _ apispec.GetMeRequestObject) (apispec.GetMeResponseObject, error) {
	sess, _ := FromContext(ctx)
	me, err := h.me(ctx, sess)
	if err != nil {
		return nil, err
	}
	return apispec.GetMe200JSONResponse(me), nil
}

func (h *Handler) ListMySessions(ctx context.Context, _ apispec.ListMySessionsRequestObject) (apispec.ListMySessionsResponseObject, error) {
	sess, _ := FromContext(ctx)
	rows, err := h.svc.ListSessions(ctx, sess)
	if err != nil {
		return nil, err
	}

	items := make([]apispec.SessionInfo, 0, len(rows))
	for _, r := range rows {
		item := apispec.SessionInfo{
			ID:         r.ID,
			Current:    r.ID == sess.ID,
			UserAgent:  r.UserAgent,
			CreatedAt:  r.CreatedAt,
			LastSeenAt: r.LastSeenAt,
		}
		if r.IP != nil {
			ip := r.IP.String()
			item.IP = &ip
		}
		items = append(items, item)
	}
	return apispec.ListMySessions200JSONResponse{Items: items}, nil
}

func (h *Handler) EndSession(ctx context.Context, req apispec.EndSessionRequestObject) (apispec.EndSessionResponseObject, error) {
	sess, _ := FromContext(ctx)
	err := h.svc.EndSession(ctx, sess, req.ID)
	if errors.Is(err, ErrNoSession) {
		return nil, echo.NewHTTPError(http.StatusNotFound, "Сеанс не найден или уже завершён")
	}
	if err != nil {
		return nil, err
	}
	return apispec.EndSession204Response{}, nil
}

func (h *Handler) EndOtherSessions(ctx context.Context, _ apispec.EndOtherSessionsRequestObject) (apispec.EndOtherSessionsResponseObject, error) {
	sess, _ := FromContext(ctx)
	if err := h.svc.EndOtherSessions(ctx, sess); err != nil {
		return nil, err
	}
	return apispec.EndOtherSessions204Response{}, nil
}

var changePasswordErrors = map[error]string{
	ErrWrongPassword:        "Текущий пароль указан неверно",
	ErrSamePassword:         "Новый пароль должен отличаться от текущего",
	password.ErrTooShort:    "Пароль должен быть не короче 8 символов",
	password.ErrTooLong:     "Пароль должен быть не длиннее 128 символов",
	password.ErrSameAsLogin: "Пароль не должен совпадать с логином",
}

func (h *Handler) ChangePassword(ctx context.Context, req apispec.ChangePasswordRequestObject) (apispec.ChangePasswordResponseObject, error) {
	sess, _ := FromContext(ctx)
	err := h.svc.ChangePassword(ctx, sess, req.Body.CurrentPassword, req.Body.NewPassword)
	if err != nil {
		for target, msg := range changePasswordErrors {
			if errors.Is(err, target) {
				return nil, echo.NewHTTPError(http.StatusUnprocessableEntity, msg)
			}
		}
		return nil, err
	}
	return apispec.ChangePassword204Response{}, nil
}

func (h *Handler) me(ctx context.Context, s Session) (apispec.Me, error) {
	locations, err := h.svc.WorkLocations(ctx, s)
	if err != nil {
		return apispec.Me{}, err
	}
	return apispec.Me{
		ID:                 s.UserID,
		Login:              s.Login,
		FullName:           s.FullName,
		Role:               apispec.Role(s.Role),
		MustChangePassword: s.MustChangePassword,
		CsrfToken:          s.CSRFToken,
		Permissions:        permissionsOf(s.Role),
		// В JSON — [], а не null.
		LocationIDs: append([]uuid.UUID{}, locations...),
	}, nil
}
