package pos

import (
	"context"
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/pos/store"
)

// StaffHandler — регистрация касс и PIN кассиров в основном приложении (ПБ-4).
type StaffHandler struct {
	svc *Service
}

func NewStaffHandler(svc *Service) *StaffHandler {
	return &StaffHandler{svc: svc}
}

var staffProblems = map[error]*echo.HTTPError{
	ErrNotFound:       echo.NewHTTPError(http.StatusNotFound, "Не найдено"),
	ErrInvalidPIN:     echo.NewHTTPError(http.StatusUnprocessableEntity, "PIN — от 4 до 6 цифр"),
	ErrNotStore:       echo.NewHTTPError(http.StatusUnprocessableEntity, "Кассу можно зарегистрировать только в магазине"),
	ErrInactivePoint:  echo.NewHTTPError(http.StatusUnprocessableEntity, "Точка в архиве"),
	auth.ErrForbidden: echo.NewHTTPError(http.StatusForbidden, "Кассы этого магазина регистрирует его управляющий или администратор"),
}

func staffProblem(err error) error {
	for target, he := range staffProblems {
		if errors.Is(err, target) {
			return he
		}
	}
	return err
}

func device(d store.ListDevicesRow) apispec.PosDevice {
	return apispec.PosDevice{
		ID: d.ID, LocationID: d.LocationID, LocationCode: d.LocationCode, LocationName: d.LocationName, Name: d.Name,
		KeyPrefix: d.KeyPrefix, CreatedAt: d.CreatedAt, LastSeenAt: d.LastSeenAt, RevokedAt: d.RevokedAt,
	}
}

func (h *StaffHandler) ListPosDevices(ctx context.Context, _ apispec.ListPosDevicesRequestObject) (apispec.ListPosDevicesResponseObject, error) {
	rows, err := h.svc.Devices(ctx)
	if err != nil {
		return nil, err
	}
	out := make(apispec.ListPosDevices200JSONResponse, len(rows))
	for i, d := range rows {
		out[i] = device(d)
	}
	return out, nil
}

func (h *StaffHandler) CreatePosDevice(ctx context.Context, req apispec.CreatePosDeviceRequestObject) (apispec.CreatePosDeviceResponseObject, error) {
	d, key, err := h.svc.RegisterDevice(ctx, req.Body.LocationID, req.Body.Name)
	if err != nil {
		return nil, staffProblem(err)
	}
	return apispec.CreatePosDevice201JSONResponse{Device: device(d), Key: key}, nil
}

func (h *StaffHandler) RevokePosDevice(ctx context.Context, req apispec.RevokePosDeviceRequestObject) (apispec.RevokePosDeviceResponseObject, error) {
	if err := h.svc.RevokeDevice(ctx, req.ID); err != nil {
		return nil, staffProblem(err)
	}
	return apispec.RevokePosDevice204Response{}, nil
}

func (h *StaffHandler) SetUserPin(ctx context.Context, req apispec.SetUserPinRequestObject) (apispec.SetUserPinResponseObject, error) {
	if err := h.svc.SetPIN(ctx, req.ID, req.Body.Pin); err != nil {
		return nil, staffProblem(err)
	}
	return apispec.SetUserPin204Response{}, nil
}
