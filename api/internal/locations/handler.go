package locations

import (
	"context"
	"fmt"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/locations/store"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) ListLocations(ctx context.Context, _ apispec.ListLocationsRequestObject) (apispec.ListLocationsResponseObject, error) {
	rows, err := h.svc.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list locations: %w", err)
	}

	// Не nil: пустой список должен уйти клиенту как [], а не null.
	items := make([]apispec.Location, 0, len(rows))
	for _, r := range rows {
		items = append(items, toAPI(r))
	}
	return apispec.ListLocations200JSONResponse{Items: items}, nil
}

func toAPI(r store.ListLocationsRow) apispec.Location {
	return apispec.Location{
		ID:       r.ID,
		Code:     r.Code,
		Name:     r.Name,
		Kind:     apispec.LocationKind(r.Kind),
		City:     r.City,
		Timezone: r.Timezone,
	}
}
