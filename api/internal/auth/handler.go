package auth

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strings"

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
	var challenge *ChallengeError
	switch {
	case errors.As(err, &challenge):
		return apispec.Login202JSONResponse{Challenge: challenge.Token, ExpiresAt: challenge.ExpiresAt}, nil
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
		TotpEnabled:        &s.TOTPEnabled,
		MustEnrollTotp:     ptr(s.MustEnrollTOTP()),
		CsrfToken:          s.CSRFToken,
		Permissions:        permissionsOf(s.Role),
		// В JSON — [], а не null.
		LocationIDs: append([]uuid.UUID{}, locations...),
	}, nil
}

func codeProblem(err error) error {
	switch {
	case errors.Is(err, ErrBadCode):
		return echo.NewHTTPError(http.StatusUnauthorized, "Неверный или просроченный код")
	case errors.Is(err, ErrBlocked):
		return echo.NewHTTPError(http.StatusForbidden, "Учётная запись заблокирована. Обратитесь к администратору")
	case errors.Is(err, ErrTOTPRequiredFor):
		return echo.NewHTTPError(http.StatusForbidden, "Администратору двухфакторная аутентификация обязательна")
	case errors.Is(err, ErrNoPendingTOTP):
		return echo.NewHTTPError(http.StatusConflict, "Сначала начните подключение — отсканируйте новый QR-код")
	}
	return err
}

func (h *Handler) signedIn(ctx context.Context, sess Session, token string) (apispec.Me, string, error) {
	me, err := h.me(ctx, sess)
	return me, sessionCookie(token, int(sessionLifetime.Seconds())), err
}

func (h *Handler) LoginTotp(ctx context.Context, req apispec.LoginTotpRequestObject) (apispec.LoginTotpResponseObject, error) {
	sess, token, err := h.svc.LoginTOTP(ctx, req.Body.Challenge, req.Body.Code, ClientFrom(ctx))
	if err != nil {
		return nil, codeProblem(err)
	}
	me, cookie, err := h.signedIn(ctx, sess, token)
	if err != nil {
		return nil, err
	}
	return apispec.LoginTotp200JSONResponse{Body: me, Headers: apispec.LoginTotp200ResponseHeaders{SetCookie: &cookie}}, nil
}

func (h *Handler) SetupTotp(ctx context.Context, _ apispec.SetupTotpRequestObject) (apispec.SetupTotpResponseObject, error) {
	sess, _ := FromContext(ctx)
	secret, url, err := h.svc.SetupTOTP(ctx, sess)
	if err != nil {
		return nil, err
	}
	return apispec.SetupTotp200JSONResponse{Secret: secret, URL: url}, nil
}

func (h *Handler) EnableTotp(ctx context.Context, req apispec.EnableTotpRequestObject) (apispec.EnableTotpResponseObject, error) {
	sess, _ := FromContext(ctx)
	codes, err := h.svc.EnableTOTP(ctx, sess, req.Body.Code)
	if err != nil {
		return nil, codeProblem(err)
	}
	return apispec.EnableTotp200JSONResponse{RecoveryCodes: codes}, nil
}

func (h *Handler) DisableTotp(ctx context.Context, req apispec.DisableTotpRequestObject) (apispec.DisableTotpResponseObject, error) {
	sess, _ := FromContext(ctx)
	if err := h.svc.DisableTOTP(ctx, sess, req.Body.Code); err != nil {
		return nil, codeProblem(err)
	}
	return apispec.DisableTotp204Response{}, nil
}

func (h *Handler) IssueQrLogin(ctx context.Context, _ apispec.IssueQrLoginRequestObject) (apispec.IssueQrLoginResponseObject, error) {
	sess, _ := FromContext(ctx)
	code, expires, err := h.svc.IssueQR(ctx, sess)
	if err != nil {
		return nil, err
	}
	return apispec.IssueQrLogin200JSONResponse{Code: code, ExpiresAt: expires}, nil
}

func (h *Handler) RedeemQrLogin(ctx context.Context, req apispec.RedeemQrLoginRequestObject) (apispec.RedeemQrLoginResponseObject, error) {
	sess, token, err := h.svc.RedeemQR(ctx, req.Body.Code, ClientFrom(ctx))
	if err != nil {
		return nil, codeProblem(err)
	}
	me, cookie, err := h.signedIn(ctx, sess, token)
	if err != nil {
		return nil, err
	}
	return apispec.RedeemQrLogin200JSONResponse{Body: me, Headers: apispec.RedeemQrLogin200ResponseHeaders{SetCookie: &cookie}}, nil
}

func keyToAPI(k APIKey) apispec.APIKey {
	scopes := make([]apispec.APIKeyScope, len(k.Scopes))
	for i, s := range k.Scopes {
		scopes[i] = apispec.APIKeyScope(s)
	}
	return apispec.APIKey{ID: k.ID, Name: k.Name, Prefix: k.Prefix, Scopes: scopes, CreatedAt: k.CreatedAt,
		CreatedByName: k.Author, LastUsedAt: k.LastUsedAt, RevokedAt: k.RevokedAt}
}

func (h *Handler) ListAPIKeys(ctx context.Context, _ apispec.ListAPIKeysRequestObject) (apispec.ListAPIKeysResponseObject, error) {
	keys, err := h.svc.APIKeys(ctx)
	if err != nil {
		return nil, err
	}
	out := apispec.ListAPIKeys200JSONResponse{Items: make([]apispec.APIKey, len(keys))}
	for i, k := range keys {
		out.Items[i] = keyToAPI(k)
	}
	return out, nil
}

func (h *Handler) CreateAPIKey(ctx context.Context, req apispec.CreateAPIKeyRequestObject) (apispec.CreateAPIKeyResponseObject, error) {
	scopes := make([]string, 0, len(req.Body.Scopes))
	for _, s := range req.Body.Scopes {
		if !slices.Contains(scopes, string(s)) {
			scopes = append(scopes, string(s))
		}
	}
	if strings.TrimSpace(req.Body.Name) == "" || len(scopes) == 0 {
		return nil, echo.NewHTTPError(http.StatusUnprocessableEntity, "Укажите название и хотя бы одну область доступа")
	}
	key, k, err := h.svc.CreateAPIKey(ctx, req.Body.Name, scopes)
	if err != nil {
		return nil, err
	}
	return apispec.CreateAPIKey201JSONResponse{Key: key, APIKey: keyToAPI(k)}, nil
}

func (h *Handler) RevokeAPIKey(ctx context.Context, req apispec.RevokeAPIKeyRequestObject) (apispec.RevokeAPIKeyResponseObject, error) {
	if err := h.svc.RevokeAPIKey(ctx, req.ID); err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, echo.NewHTTPError(http.StatusNotFound, "Ключ не найден")
		}
		return nil, err
	}
	return apispec.RevokeAPIKey204Response{}, nil
}

func (h *Handler) ListAPIKeyRequests(ctx context.Context, req apispec.ListAPIKeyRequestsRequestObject) (apispec.ListAPIKeyRequestsResponseObject, error) {
	rows, err := h.svc.APIKeyRequests(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	out := apispec.ListAPIKeyRequests200JSONResponse{Items: make([]apispec.APIKeyRequest, len(rows))}
	for i, r := range rows {
		out.Items[i] = apispec.APIKeyRequest{At: r.At, Method: r.Method, Operation: r.Operation, IP: r.IP, Ok: r.OK}
	}
	return out, nil
}
