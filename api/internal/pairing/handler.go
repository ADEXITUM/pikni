package pairing

import (
	"context"
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func problem(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return echo.NewHTTPError(http.StatusNotFound, "Сопряжение не найдено — откройте его на компьютере под своим пользователем")
	case errors.Is(err, ErrExpired):
		return echo.NewHTTPError(http.StatusGone, "Сопряжение закрыто или истекло — откройте новое")
	case errors.Is(err, ErrBadCode):
		return echo.NewHTTPError(http.StatusUnprocessableEntity, "Это не QR-код сопряжения")
	}
	return err
}

func (h *Handler) OpenPairing(ctx context.Context, _ apispec.OpenPairingRequestObject) (apispec.OpenPairingResponseObject, error) {
	id, code, expires, err := h.svc.Open(ctx)
	if err != nil {
		return nil, err
	}
	return apispec.OpenPairing201JSONResponse{ID: id, Code: &code, Status: apispec.PairingStatusWaiting, ExpiresAt: expires}, nil
}

func (h *Handler) JoinPairing(ctx context.Context, req apispec.JoinPairingRequestObject) (apispec.JoinPairingResponseObject, error) {
	id, expires, err := h.svc.Join(ctx, req.Body.Code)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.JoinPairing200JSONResponse{ID: id, Status: apispec.PairingStatusActive, ExpiresAt: expires}, nil
}

func (h *Handler) GetPairing(ctx context.Context, req apispec.GetPairingRequestObject) (apispec.GetPairingResponseObject, error) {
	status, expires, err := h.svc.Status(ctx, req.ID)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.GetPairing200JSONResponse{ID: req.ID, Status: apispec.PairingStatus(status), ExpiresAt: expires}, nil
}

func (h *Handler) ClosePairing(ctx context.Context, req apispec.ClosePairingRequestObject) (apispec.ClosePairingResponseObject, error) {
	if err := h.svc.Close(ctx, req.ID); err != nil {
		return nil, problem(err)
	}
	return apispec.ClosePairing204Response{}, nil
}

func (h *Handler) SendPairedScan(ctx context.Context, req apispec.SendPairedScanRequestObject) (apispec.SendPairedScanResponseObject, error) {
	if err := h.svc.Scan(ctx, req.ID, req.Body.ScanID, req.Body.Raw); err != nil {
		return nil, problem(err)
	}
	return apispec.SendPairedScan204Response{}, nil
}
