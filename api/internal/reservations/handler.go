package reservations

import (
	"context"
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/reservations/store"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

var problems = map[error]*echo.HTTPError{
	ErrNotFound:       echo.NewHTTPError(http.StatusNotFound, "Бронь не найдена"),
	ErrConsent:        echo.NewHTTPError(http.StatusUnprocessableEntity, "Без согласия покупателя на обработку персональных данных бронь не оформить"),
	ErrBadCustomer:    echo.NewHTTPError(http.StatusUnprocessableEntity, "Укажите имя покупателя, телефон и количество"),
	ErrNotOpen:        echo.NewHTTPError(http.StatusConflict, "Бронь уже подтверждена, выдана, снята или истекла"),
	ErrNotConfirmed:   echo.NewHTTPError(http.StatusConflict, "Бронь не подтверждена — товар не отложен"),
	ErrWrongItem:      echo.NewHTTPError(http.StatusUnprocessableEntity, "Отсканирован другой товар — нужен товар из брони"),
	ErrWrongCount:     echo.NewHTTPError(http.StatusUnprocessableEntity, "Отсканируйте марку каждой отложенной бутылки — столько, сколько в брони"),
	ErrNotEnough:      echo.NewHTTPError(http.StatusConflict, "Свободного товара не хватает или марка не на остатке точки"),
	ErrInactive:       echo.NewHTTPError(http.StatusUnprocessableEntity, "Точка или товар в архиве"),
	ErrNotStore:       echo.NewHTTPError(http.StatusUnprocessableEntity, "Бронь оформляется в магазине"),
	ErrWrongLocation:  echo.NewHTTPError(http.StatusConflict, "Бронь оформлена в другой точке"),
	auth.ErrForbidden: echo.NewHTTPError(http.StatusForbidden, "Недостаточно прав для этой точки"),
}

func problem(err error) error {
	for target, he := range problems {
		if errors.Is(err, target) {
			return he
		}
	}
	return err
}

func toAPI(r store.ListReservationsRow) apispec.Reservation {
	out := apispec.Reservation{
		ID: r.ID, Number: r.Number, LocationID: r.LocationID, LocationCode: r.LocationCode, LocationName: r.LocationName,
		VariantID: r.VariantID, Article: r.Article, Name: r.Name,
		Label: catalog.PackLabel(r.VolumeMl, r.NetWeightG, r.Container, r.GiftPack),
		Qty:   r.Qty, CustomerName: r.CustomerName, CustomerPhone: r.CustomerPhone,
		Status: apispec.ReservationStatus(r.Status), ExpiresAt: r.ExpiresAt, Marks: r.Marks, Comment: &r.Comment,
		CreatedAt: r.CreatedAt, CreatedByName: r.CreatedByName,
	}
	if r.EgaisAccounting != nil {
		a := apispec.EgaisAccounting(*r.EgaisAccounting)
		out.Accounting = &a
	}
	if r.ZoneKind != nil {
		z := apispec.ZoneKind(*r.ZoneKind)
		out.ZoneKind = &z
	}
	if r.ReceiptNumber != nil {
		n := int(*r.ReceiptNumber)
		out.ReceiptNumber = &n
	}
	return out
}

func (h *Handler) ListReservations(ctx context.Context, req apispec.ListReservationsRequestObject) (apispec.ListReservationsResponseObject, error) {
	rows, err := h.svc.List(ctx, req.Params.LocationID, req.Params.Open != nil && *req.Params.Open)
	if err != nil {
		return nil, problem(err)
	}
	out := apispec.ListReservations200JSONResponse{Items: make([]apispec.Reservation, len(rows))}
	for i, r := range rows {
		out.Items[i] = toAPI(r)
	}
	return out, nil
}

func (h *Handler) CreateReservation(ctx context.Context, req apispec.CreateReservationRequestObject) (apispec.CreateReservationResponseObject, error) {
	b := req.Body
	n := New{
		LocationID: b.LocationID, VariantID: b.VariantID, Qty: b.Qty, CustomerName: b.CustomerName,
		Phone: b.CustomerPhone, Consent: b.Consent,
	}
	if b.Comment != nil {
		n.Comment = *b.Comment
	}
	id, err := h.svc.Create(ctx, n)
	if err != nil {
		return nil, problem(err)
	}
	r, err := h.svc.Get(ctx, id)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.CreateReservation201JSONResponse(toAPI(r)), nil
}

func (h *Handler) ConfirmReservation(ctx context.Context, req apispec.ConfirmReservationRequestObject) (apispec.ConfirmReservationResponseObject, error) {
	if err := h.svc.Confirm(ctx, req.ID, req.Body.Codes); err != nil {
		return nil, problem(err)
	}
	r, err := h.svc.Get(ctx, req.ID)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.ConfirmReservation200JSONResponse(toAPI(r)), nil
}

func (h *Handler) CancelReservation(ctx context.Context, req apispec.CancelReservationRequestObject) (apispec.CancelReservationResponseObject, error) {
	if err := h.svc.Cancel(ctx, req.ID); err != nil {
		return nil, problem(err)
	}
	r, err := h.svc.Get(ctx, req.ID)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.CancelReservation200JSONResponse(toAPI(r)), nil
}
