package reconcile

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

func problem(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return echo.NewHTTPError(http.StatusNotFound, "Точка не найдена")
	case errors.Is(err, ErrNoFSRAR):
		return echo.NewHTTPError(http.StatusUnprocessableEntity, "У точки не указан ФСРАР ИД — запросить остатки в ЕГАИС нельзя")
	case errors.Is(err, auth.ErrForbidden):
		return echo.NewHTTPError(http.StatusForbidden, "Недостаточно прав для этой точки")
	}
	return err
}

func (h *Handler) RequestRests(ctx context.Context, req apispec.RequestRestsRequestObject) (apispec.RequestRestsResponseObject, error) {
	if err := h.svc.Request(ctx, req.LocationID); err != nil {
		return nil, problem(err)
	}
	return apispec.RequestRests202Response{}, nil
}

func (h *Handler) GetReconciliation(ctx context.Context, req apispec.GetReconciliationRequestObject) (apispec.GetReconciliationResponseObject, error) {
	r, err := h.svc.Report(ctx, req.LocationID)
	if err != nil {
		return nil, problem(err)
	}
	out := apispec.GetReconciliation200JSONResponse{
		TakenAt: r.TakenAt, RequestedAt: r.RequestedAt,
		Products: make([]apispec.ReconciliationProduct, len(r.Products)),
		Marks:    make([]apispec.ReconciliationMark, len(r.Marks)),
	}
	for i, p := range r.Products {
		out.Products[i] = apispec.ReconciliationProduct{
			Alcocode: p.Alcocode, Name: p.Name, Register: apispec.ReconciliationProductRegister(p.Register), Ours: p.Ours, Egais: p.Egais,
		}
	}
	for i, m := range r.Marks {
		out.Marks[i] = apispec.ReconciliationMark{Code: m.Code, Alcocode: m.Alcocode, Name: m.Name, OnlyOurs: m.OnlyOurs,
			DocumentID: m.DocumentID, DocumentNumber: m.DocumentNumber}
		if m.Status != nil {
			st := apispec.MarkStatus(*m.Status)
			out.Marks[i].Status = &st
		}
	}
	return out, nil
}
