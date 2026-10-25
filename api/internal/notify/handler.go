package notify

import (
	"context"
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) ListNotifications(ctx context.Context, req apispec.ListNotificationsRequestObject) (apispec.ListNotificationsResponseObject, error) {
	items, unread, err := h.svc.List(ctx, req.Params.Unread != nil && *req.Params.Unread)
	if err != nil {
		return nil, err
	}
	out := apispec.ListNotifications200JSONResponse{Items: make([]apispec.Notification, len(items)), Unread: unread}
	for i, n := range items {
		out.Items[i] = apispec.Notification{ID: n.ID, Kind: n.Kind, Title: n.Title, Body: n.Body, Link: n.Link,
			CreatedAt: n.CreatedAt, ReadAt: n.ReadAt}
	}
	return out, nil
}

func (h *Handler) ReadNotifications(ctx context.Context, req apispec.ReadNotificationsRequestObject) (apispec.ReadNotificationsResponseObject, error) {
	var id *openapi_types.UUID
	if req.Body != nil {
		id = req.Body.ID
	}
	if err := h.svc.Read(ctx, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, echo.NewHTTPError(http.StatusNotFound, "Уведомление не найдено")
		}
		return nil, err
	}
	return apispec.ReadNotifications204Response{}, nil
}
